package node

import (
	"context"
	"io"
	"log/slog"
	"mime"
	"path"
	"time"

	"github.com/pion/webrtc/v4"

	"github.com/betterbotbureau/linkus/internal/protocol"
	"github.com/betterbotbureau/linkus/internal/site"
)

// Backpressure bounds for sending chunks: sending pauses while more than
// sendHighWater is buffered and resumes when the channel drains below
// sendLowWater, so a whole file is never queued at once.
const (
	sendHighWater = 1 << 20   // 1 MB
	sendLowWater  = 256 << 10 // 256 KB
)

// Stall detection; variables so tests can shorten them.
var (
	sendStallTimeout = 30 * time.Second // with nothing drained at all
	stallCheckEvery  = 500 * time.Millisecond
)

// fetchDataChannel is the part of *webrtc.DataChannel the fetch server
// uses, as an interface so the protocol has tests without WebRTC.
type fetchDataChannel interface {
	Send([]byte) error
	SendText(string) error
	BufferedAmount() uint64
	SetBufferedAmountLowThreshold(uint64)
	OnBufferedAmountLow(func())
	Close() error
}

// fetchServer answers linkus-fetch-v1 requests on one data channel, one at
// a time, through the same site rules as the local preview.
type fetchServer struct {
	dc       fetchDataChannel
	st       site.Opener
	stats    *visitStats
	log      *slog.Logger
	requests chan fetchMessage
	low      chan struct{}
	ctx      context.Context
	stop     context.CancelFunc
}

type fetchMessage struct {
	text bool
	data []byte
}

// newFetchServer starts serving dc until ctx ends, the remote closes, or a
// protocol violation closes the channel.
func newFetchServer(ctx context.Context, dc fetchDataChannel, st site.Opener, stats *visitStats, log *slog.Logger) *fetchServer {
	fctx, cancel := context.WithCancel(ctx)
	f := &fetchServer{
		dc:    dc,
		st:    st,
		stats: stats,
		log:   log,
		// One request being served plus one queued; a third concurrent
		// request violates "one request at a time" and closes the channel.
		requests: make(chan fetchMessage, 1),
		low:      make(chan struct{}, 1),
		ctx:      fctx,
		stop:     cancel,
	}
	dc.SetBufferedAmountLowThreshold(sendLowWater)
	dc.OnBufferedAmountLow(func() {
		select {
		case f.low <- struct{}{}:
		default:
		}
	})
	go f.run()
	return f
}

// handleMessage takes one incoming message; it never blocks.
func (f *fetchServer) handleMessage(text bool, data []byte) {
	select {
	case f.requests <- fetchMessage{text: text, data: data}:
	case <-f.ctx.Done():
	default:
		f.log.Warn("fetch channel flooded, closing")
		f.stop()
	}
}

func (f *fetchServer) run() {
	defer f.dc.Close()
	defer f.stop()
	for {
		select {
		case <-f.ctx.Done():
			return
		case msg := <-f.requests:
			if !f.serve(msg) {
				return
			}
		}
	}
}

// serve answers one request. It returns false when the channel must close:
// oversized or unknown-type requests, send failures, stalled visitors.
func (f *fetchServer) serve(msg fetchMessage) bool {
	// Requests are text; a binary request is an unknown type.
	if !msg.text || len(msg.data) > protocol.MaxFetchRequestBytes {
		f.log.Warn("closing fetch channel: bad request message")
		return false
	}
	req, err := protocol.DecodeFetchRequest(msg.data)
	if err != nil || req.V != protocol.SignalVersion || req.Type != protocol.FetchGet {
		f.log.Warn("closing fetch channel: unknown request type")
		return false
	}
	f.stats.request(req.Path)

	file, err := f.st.Open(req.Path)
	if err != nil {
		// Everything unsafe or missing is the same plain 404 the preview
		// would give; the channel stays open for further requests.
		return f.sendJSON(protocol.FetchErrorMessage{
			V:      protocol.SignalVersion,
			Type:   protocol.FetchError,
			Status: 404,
		})
	}
	defer file.Close()

	ctype := mime.TypeByExtension(path.Ext(file.Name))
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	ok := f.sendJSON(protocol.FetchHeadMessage{
		V:           protocol.SignalVersion,
		Type:        protocol.FetchHead,
		Status:      200,
		Path:        req.Path,
		Size:        file.Size,
		ContentType: ctype,
	})
	if !ok {
		return false
	}

	buf := make([]byte, protocol.FetchChunkSize)
	for {
		n, err := file.Read(buf)
		if n > 0 {
			if !f.waitForBuffer() {
				f.log.Warn("closing fetch channel: visitor not reading")
				return false
			}
			// Copy: the SCTP layer keeps the slice for retransmission.
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			if f.dc.Send(chunk) != nil {
				return false
			}
			f.stats.sent(n)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			// Mid-stream read failure after a 200 head: nothing honest to
			// say but closing.
			f.log.Warn("closing fetch channel: read failed", "error", err)
			return false
		}
	}
	return f.sendJSON(protocol.FetchDoneMessage{
		V:    protocol.SignalVersion,
		Type: protocol.FetchDone,
	})
}

type wireEncoder interface{ Encode() ([]byte, error) }

func (f *fetchServer) sendJSON(m wireEncoder) bool {
	data, err := m.Encode()
	if err != nil {
		return false
	}
	return f.dc.SendText(string(data)) == nil
}

// waitForBuffer blocks until the channel's send buffer has room, the
// visitor stalls too long, or the visit ends.
func (f *fetchServer) waitForBuffer() bool {
	return waitForRoom(f.ctx, f.dc, f.low)
}

// waitForRoom waits while more than sendHighWater is queued on dc. A slow
// visitor is fine — a phone far away may drain a megabyte over half a
// minute — so it gives up only when nothing at all has drained for
// sendStallTimeout. The low-water event says when the buffer is nearly
// empty; the ticker notices the progress in between.
func waitForRoom(ctx context.Context, dc fetchDataChannel, low <-chan struct{}) bool {
	last := dc.BufferedAmount()
	if last <= sendHighWater {
		return true
	}
	moved := time.Now()
	tick := time.NewTicker(stallCheckEvery)
	defer tick.Stop()
	for {
		select {
		case <-low:
		case <-tick.C:
		case <-ctx.Done():
			return false
		}
		cur := dc.BufferedAmount()
		if cur <= sendHighWater {
			return true
		}
		if cur < last {
			last, moved = cur, time.Now()
		} else if time.Since(moved) >= sendStallTimeout {
			return false
		}
	}
}

// serveFetchChannel wires a real WebRTC data channel to a fetch server for
// the lifetime of the visit.
func serveFetchChannel(ctx context.Context, dc *webrtc.DataChannel, st site.Opener, stats *visitStats, log *slog.Logger) {
	f := newFetchServer(ctx, dc, st, stats, log.With("channel", dc.Label()))
	dc.OnMessage(func(m webrtc.DataChannelMessage) {
		f.handleMessage(m.IsString, m.Data)
	})
	dc.OnClose(f.stop)
}
