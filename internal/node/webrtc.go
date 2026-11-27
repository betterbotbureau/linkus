package node

import (
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"net"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/ice/v4"
	"github.com/pion/webrtc/v4"

	"github.com/betterbotbureau/linkus/internal/identity"
	"github.com/betterbotbureau/linkus/internal/protocol"
	"github.com/betterbotbureau/linkus/internal/site"
)

// fallbackSTUN stays as a second entry during v0.1 for resilience; the
// primary is Link.us's own STUN, derived from the server URL.
const fallbackSTUN = "stun:stun.l.google.com:19302"

// STUNServers returns the node's default STUN list for the given service
// URL: the Link.us STUN on port 3478 first, one public fallback second.
func STUNServers(server string) []string {
	u, err := url.Parse(server)
	if err != nil || u.Hostname() == "" {
		return []string{fallbackSTUN}
	}
	return []string{"stun:" + u.Hostname() + ":3478", fallbackSTUN}
}

const (
	// gatherTimeout bounds non-trickle ICE gathering; whatever candidates
	// exist by then are sent.
	gatherTimeout = 10 * time.Second
	// connectTimeout is how long after answering the node waits for the
	// visitor's connection to come up before abandoning the visit.
	connectTimeout = 45 * time.Second

	// A connection is "disconnected" after iceDisconnected without
	// packets and "failed" after iceFailed. Disconnected is a pause (a
	// Wi-Fi blip, a phone radio asleep) that usually recovers, so a visit
	// ends only when the connection fails or closes. iceKeepalive keeps
	// NAT mappings open while a visitor just reads.
	iceDisconnected = 5 * time.Second
	iceFailed       = 30 * time.Second
	iceKeepalive    = 2 * time.Second
)

// visitIdle closes a site visit that has asked for nothing this long, so
// open tabs don't hold the node's few visitor slots. The visitor never
// sees it: the page stays, and its next request reconnects. Chat visits
// stay open, since replies must reach them live.
var visitIdle = 2 * time.Minute

// WebRTC answers visitor offers over WebRTC data channels, serving files
// through the same site rules as the local preview. All traffic uses one
// UDP port chosen at startup, so router port mappings stay valid.
type WebRTC struct {
	cfg    webrtc.Configuration
	st     site.Opener
	id     *identity.Identity // signs answers; nil only in unit tests
	log    *slog.Logger
	slots  *visitSlots // bounds concurrent visits, in total and per address
	udp    *net.UDPConn
	pconn  *probeConn
	mux    ice.UDPMux
	served atomic.Int64
	active atomic.Int64

	apiMu sync.RWMutex
	api   *webrtc.API

	sendMu sync.Mutex
	send   SignalSender // the live presence connection's sender, nil offline

	reqlog   *RequestLog               // optional persistent visit log
	handlers map[string]ChannelHandler // channels beyond the site fetch, by label
	// authCheck answers the service's sign-in checks; nil: no visitor
	// may sign in.
	authCheck func(email string) bool
	// visitorOf names the visitor signed in on a visit, or ""; nil: no
	// sign-in.
	visitorOf func(v *Visit) string
}

// AuthChecker answers the service's sign-in checks.
type AuthChecker interface {
	AuthCheck(email string) bool
}

// AuthCheck reports whether an address may receive a sign-in code: the
// site has something for it.
func (w *WebRTC) AuthCheck(email string) bool {
	return w.authCheck != nil && w.authCheck(email)
}

// Visit is what a channel handler knows about the visit it serves.
type Visit struct {
	Ctx context.Context
	Log *slog.Logger
	// RemoteIP is the visitor's address, as far as it is known.
	RemoteIP func() string
	stats    *visitStats
	stayOpen atomic.Bool

	mu     sync.Mutex
	state  map[string]any
	offer  string
	answer string
}

// Exchange returns the visit's offer and the node's answer, so a channel
// can bind a proof to this very connection. The answer is set before any
// channel can open.
func (v *Visit) Exchange() (offer, answer string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.offer, v.answer
}

// StayOpen keeps the visit open while it is idle: a channel that waits on
// the visitor (a chat) rather than serving requests.
func (v *Visit) StayOpen() { v.stayOpen.Store(true) }

// Shared returns the visit's value under key, made by mk the first time;
// the channels of one visit share it (a sign-in, a one-chat-per-visit
// mark).
func (v *Visit) Shared(key string, mk func() any) any {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.state == nil {
		v.state = map[string]any{}
	}
	x, ok := v.state[key]
	if !ok {
		x = mk()
		v.state[key] = x
	}
	return x
}

// ChannelHandler serves one data channel a visitor opened. It must not
// block: it sets the channel's callbacks and returns.
type ChannelHandler func(v *Visit, dc *webrtc.DataChannel)

// HandleChannel serves visitors' channels labelled label with h. Call it
// before visits are served. Without a handler, a channel of another Link
// feature (a "linkus-" label other than the site fetch) is closed, and
// every other channel is a site fetch.
func (w *WebRTC) HandleChannel(label string, h ChannelHandler) {
	if w.handlers == nil {
		w.handlers = map[string]ChannelHandler{}
	}
	w.handlers[label] = h
}

// otherFeature reports a channel of a Link feature this node does not
// serve.
func otherFeature(label string) bool {
	return strings.HasPrefix(label, "linkus-") && label != protocol.FetchProtocol
}

// SetRequestLog makes visits land in the persistent request log. Call it
// before serving.
func (w *WebRTC) SetRequestLog(l *RequestLog) { w.reqlog = l }

// NewWebRTC binds the node's WebRTC UDP port and prepares the answering
// side. port is the preferred port from the last run (0 picks a fresh one;
// a taken port falls back to a fresh one too). stunServers may be nil
// (loopback tests need none); id signs every answer and may be nil only in
// tests.
func NewWebRTC(st site.Opener, id *identity.Identity, stunServers []string, port int, log *slog.Logger) (*WebRTC, error) {
	udp, err := net.ListenUDP("udp", &net.UDPAddr{Port: port})
	if err != nil && port != 0 {
		log.Info("remembered UDP port taken, picking a new one", "port", port)
		udp, err = net.ListenUDP("udp", &net.UDPAddr{Port: 0})
	}
	if err != nil {
		return nil, err
	}
	pconn := newProbeConn(udp)
	cfg := webrtc.Configuration{}
	for _, u := range stunServers {
		cfg.ICEServers = append(cfg.ICEServers, webrtc.ICEServer{URLs: []string{u}})
	}
	w := &WebRTC{
		cfg:   cfg,
		st:    st,
		id:    id,
		log:   log,
		slots: newVisitSlots(protocol.DefaultMaxVisits),
		udp:   udp,
		pconn: pconn,
		mux:   webrtc.NewICEUDPMux(nil, pconn),
	}
	w.api = w.buildAPI("")
	return w, nil
}

// buildAPI assembles the Pion API for the shared mux, with the verified
// external IP as an extra server-reflexive candidate when one is known.
func (w *WebRTC) buildAPI(externalIP string) *webrtc.API {
	se := webrtc.SettingEngine{}
	se.SetICEUDPMux(w.mux)
	se.SetICETimeouts(iceDisconnected, iceFailed, iceKeepalive)
	se.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4, webrtc.NetworkTypeUDP6})
	if externalIP != "" {
		se.SetNAT1To1IPs([]string{externalIP}, webrtc.ICECandidateTypeSrflx)
	}
	return webrtc.NewAPI(webrtc.WithSettingEngine(se))
}

// SetExternalIP advertises (or with "", stops advertising) a verified
// mapped address as an extra candidate on future visits. Visits already
// running are not touched.
func (w *WebRTC) SetExternalIP(ip string) {
	w.apiMu.Lock()
	defer w.apiMu.Unlock()
	w.api = w.buildAPI(ip)
}

func (w *WebRTC) currentAPI() *webrtc.API {
	w.apiMu.RLock()
	defer w.apiMu.RUnlock()
	return w.api
}

// SetSender gives the WebRTC side the live presence connection's sender
// (nil when the connection drops). PresenceLoop calls this.
func (w *WebRTC) SetSender(s SignalSender) {
	w.sendMu.Lock()
	defer w.sendMu.Unlock()
	w.send = s
}

// Connected reports whether a presence connection to the server is live
// (the sender exists exactly while one is).
func (w *WebRTC) Connected() bool {
	w.sendMu.Lock()
	defer w.sendMu.Unlock()
	return w.send != nil
}

// SendSignal sends one message over the current presence connection.
func (w *WebRTC) SendSignal(m protocol.SignalMessage) error {
	w.sendMu.Lock()
	s := w.send
	w.sendMu.Unlock()
	if s == nil {
		return errors.New("not connected to the server")
	}
	return s(m)
}

// ExpectProbe arms the socket wrapper for one verification probe.
func (w *WebRTC) ExpectProbe(nonce string) <-chan struct{} { return w.pconn.Expect(nonce) }

// Port returns the UDP port all WebRTC traffic uses.
func (w *WebRTC) Port() int { return w.udp.LocalAddr().(*net.UDPAddr).Port }

// VisitsServed returns how many visits reached a connected state since start.
func (w *WebRTC) VisitsServed() int64 { return w.served.Load() }

// SetMaxVisits changes how many visitors are served at once; open visits
// are never ended by a lower number.
func (w *WebRTC) SetMaxVisits(n int) { w.slots.setMax(n) }

// VisitLimits returns the limit in total and per address.
func (w *WebRTC) VisitLimits() (total, perAddr int) { return w.slots.limits() }

// ActiveVisits returns how many visitors are connected right now.
func (w *WebRTC) ActiveVisits() int64 { return w.active.Load() }

// Close releases the UDP port.
func (w *WebRTC) Close() error { return w.udp.Close() }

// HandleVisitOffer validates one relayed offer and answers it on its own
// goroutine. Everything arriving here is hostile input: type, visit ID form
// and SDP length are checked before Pion sees any of it.
// from is the visitor's address as the service saw it ("" from older
// services), used only for the per-address cap.
func (w *WebRTC) HandleVisitOffer(ctx context.Context, visitID, sdp, from string, send SignalSender) {
	if !protocol.ValidVisitID(visitID) || sdp == "" || len(sdp) > protocol.MaxSDPBytes {
		w.log.Warn("dropping invalid visit offer")
		return
	}
	addr := addressKey(from)
	if ok, why := w.slots.take(addr); !ok {
		w.log.Info("visit refused, at capacity", "visit", visitID, "limit", why)
		if err := send(protocol.SignalMessage{
			V:     protocol.SignalVersion,
			Type:  protocol.SignalVisitBusy,
			Visit: visitID,
		}); err != nil {
			w.log.Warn("could not send busy", "visit", visitID, "error", err)
		}
		return
	}
	go func() {
		defer w.slots.release(addr)
		// A panic in one visit must never take the node down.
		defer func() {
			if r := recover(); r != nil {
				w.log.Error("visit handler panic", "visit", visitID, "panic", r)
			}
		}()
		w.serveVisit(ctx, visitID, sdp, send)
	}()
}

// serveVisit runs one visit: answer the offer, wait for the connection,
// serve data channels until the visit ends.
func (w *WebRTC) serveVisit(ctx context.Context, visitID, sdp string, send SignalSender) {
	log := w.log.With("visit", visitID)
	start := time.Now()
	stats := &visitStats{last: time.Now()}

	// visitCtx ends every goroutine belonging to this visit.
	visitCtx, cancelVisit := context.WithCancel(ctx)
	defer cancelVisit()

	pc, err := w.currentAPI().NewPeerConnection(w.cfg)
	if err != nil {
		log.Error("new peer connection", "error", err)
		return
	}
	defer pc.Close()

	var lastICE, remoteIP atomic.Value
	lastICE.Store("new")
	remoteIP.Store("")
	pc.OnICEConnectionStateChange(func(s webrtc.ICEConnectionState) {
		lastICE.Store(s.String())
	})

	connected := make(chan struct{})
	ended := make(chan struct{})
	var connOnce, endOnce sync.Once
	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		switch s {
		case webrtc.PeerConnectionStateConnected:
			connOnce.Do(func() { close(connected) })
		case webrtc.PeerConnectionStateFailed,
			webrtc.PeerConnectionStateClosed:
			endOnce.Do(func() { close(ended) })
		}
	})

	// The node only accepts channels the visitor opens; it never opens any.
	// The bootstrap shell opens a small pool; anything past the cap is
	// hostile and is closed.
	// Other features' pages open their own channels (see HandleChannel).
	var channels atomic.Int32
	visit := &Visit{Ctx: visitCtx, Log: log, stats: stats, offer: sdp, RemoteIP: func() string {
		if ip, _ := remoteIP.Load().(string); ip != "" {
			return ip
		}
		return remoteAddr(pc)
	}}
	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		if channels.Add(1) > protocol.MaxChannelsPerVisit {
			log.Info("closing excess data channel", "label", dc.Label())
			dc.Close()
			return
		}
		if h, ok := w.handlers[dc.Label()]; ok {
			h(visit, dc)
			return
		}
		if otherFeature(dc.Label()) {
			dc.Close()
			return
		}
		serveFetchChannel(visitCtx, dc, w.st, stats, log)
	})

	if err := pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  sdp,
	}); err != nil {
		log.Info("rejecting offer", "error", err)
		return
	}
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		log.Info("cannot answer offer", "error", err)
		return
	}
	gathered := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(answer); err != nil {
		log.Error("set local description", "error", err)
		return
	}
	// Non-trickle by design: wait for gathering, send one SDP.
	select {
	case <-gathered:
	case <-time.After(gatherTimeout):
		log.Warn("ICE gathering slow, answering with partial candidates")
	case <-visitCtx.Done():
		return
	}
	local := pc.LocalDescription()
	if local == nil || len(local.SDP) > protocol.MaxSDPBytes {
		log.Error("unusable local description")
		return
	}
	// Sign the answer so the visitor can verify it is talking to the key
	// holder. The offer hash binds the signature to
	// this exchange; the answer covers the DTLS fingerprint.
	visit.mu.Lock()
	visit.answer = local.SDP
	visit.mu.Unlock()
	var sig string
	if w.id != nil {
		sig = base64.StdEncoding.EncodeToString(
			w.id.Sign(protocol.AnswerSigningBytes(sdp, local.SDP)))
	}
	if err := send(protocol.SignalMessage{
		V:     protocol.SignalVersion,
		Type:  protocol.SignalVisitAnswer,
		Visit: visitID,
		SDP:   local.SDP,
		Sig:   sig,
	}); err != nil {
		log.Warn("could not send answer", "error", err)
		return
	}

	finish := func(outcome string) {
		log.Info("visit ended",
			"outcome", outcome,
			"ice", lastICE.Load(),
			"ip", remoteIP.Load(),
			"duration", time.Since(start).Round(time.Millisecond),
			"paths", stats.PathList(),
			"bytes_sent", stats.BytesSent())
		w.reqlog.Add(VisitEntry{
			Time:    start,
			Visit:   visitID,
			Outcome: outcome,
			ICE:     lastICE.Load().(string),
			IP:      remoteIP.Load().(string),
			Paths:   stats.PathList(),
			Bytes:   stats.BytesSent(),
			Secs:    float64(time.Since(start).Milliseconds()/100) / 10,
		})
	}

	select {
	case <-connected:
	case <-ended:
		finish("never connected")
		return
	case <-time.After(connectTimeout):
		finish("connect timeout")
		return
	case <-visitCtx.Done():
		finish("node shutting down")
		return
	}

	if addr := remoteAddr(pc); addr != "" {
		remoteIP.Store(addr)
	}
	w.served.Add(1)
	w.active.Add(1)
	defer w.active.Add(-1)
	log.Info("visitor connected", "ip", remoteIP.Load())
	stats.touch() // the idle clock starts when the visitor is connected
	idle := time.NewTicker(visitIdle / 8)
	defer idle.Stop()
	for {
		select {
		case <-ended:
			finish("closed")
			return
		case <-visitCtx.Done():
			finish("node shutting down")
			return
		case <-idle.C:
			if !visit.stayOpen.Load() && stats.idleFor() >= visitIdle {
				finish("idle")
				return
			}
		}
	}
}

// visitStats collects what one visit requested and received, for the node's
// log (groundwork for the v0.1 request log).
type visitStats struct {
	mu    sync.Mutex
	paths []string
	bytes int64
	last  time.Time // last request or byte sent
}

// idleFor is how long the visit has neither asked for nor received
// anything.
func (s *visitStats) idleFor() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return time.Since(s.last)
}

func (s *visitStats) touch() {
	s.mu.Lock()
	s.last = time.Now()
	s.mu.Unlock()
}

func (s *visitStats) request(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.last = time.Now()
	if len(s.paths) < 100 { // bound memory per visit
		s.paths = append(s.paths, path)
	}
}

func (s *visitStats) sent(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bytes += int64(n)
	s.last = time.Now()
}

func (s *visitStats) PathList() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.paths...)
}

func (s *visitStats) BytesSent() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bytes
}

// remoteAddr reports the visitor's address from the selected ICE candidate
// pair: their public IP across the internet, their LAN address when both
// peers are on the same network. Best effort; "" when unknown.
func remoteAddr(pc *webrtc.PeerConnection) string {
	sctp := pc.SCTP()
	if sctp == nil {
		return ""
	}
	dtls := sctp.Transport()
	if dtls == nil {
		return ""
	}
	ice := dtls.ICETransport()
	if ice == nil {
		return ""
	}
	pair, err := ice.GetSelectedCandidatePair()
	if err != nil || pair == nil || pair.Remote == nil {
		return ""
	}
	return pair.Remote.Address
}
