package node

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/betterbotbureau/linkus/internal/identity"
	"github.com/betterbotbureau/linkus/internal/protocol"
)

const (
	backoffMin = 1 * time.Second
	backoffMax = 60 * time.Second
)

// httpClient bounds every request to the service.
var httpClient = &http.Client{Timeout: 15 * time.Second}

// Register asks the service for the node's ID and stores it. It returns the
// assigned (or existing) ID.
func Register(ctx context.Context, server string, id *identity.Identity) (string, error) {
	nodeID, err := requestID(ctx, server, id)
	if err != nil {
		return "", err
	}
	if err := id.SetNodeID(nodeID); err != nil {
		return "", fmt.Errorf("store node ID: %w", err)
	}
	return nodeID, nil
}

// Confirm re-registers the node's key to tell the service the node still
// exists, and returns the ID the service holds for it without storing it.
// The service reclaims IDs that never go online and are never confirmed,
// so a node that is installed but not yet published keeps its ID by
// confirming at startup and daily.
func Confirm(ctx context.Context, server string, id *identity.Identity) (string, error) {
	return requestID(ctx, server, id)
}

// ConfirmLoop confirms once a day until ctx ends. A different ID in the
// reply means the registration was reclaimed while the node was off and
// re-created; that is logged, and the next start adopts the new ID.
func ConfirmLoop(ctx context.Context, server string, id *identity.Identity, log *slog.Logger) {
	known := id.NodeID
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(jitter(24 * time.Hour)):
		}
		got, err := Confirm(ctx, server, id)
		if err != nil {
			log.Warn("could not confirm the node's ID with the service; will retry tomorrow", "error", err)
			continue
		}
		if got != known {
			log.Warn("the service now knows this node by a different ID; restart the node to use it", "old", known, "new", got)
		}
	}
}

// requestID posts a signed registration and returns the ID in the reply.
func requestID(ctx context.Context, server string, id *identity.Identity) (string, error) {
	body, err := json.Marshal(protocol.RegisterRequest{
		PublicKey: id.PublicKeyBase64(),
		Signature: base64.StdEncoding.EncodeToString(id.Sign(protocol.RegisterSigningBytes(id.PublicKey))),
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(server, "/")+"/v1/nodes", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, protocol.MaxBodyBytes))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("registration rejected: %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	var reply protocol.RegisterResponse
	if err := json.Unmarshal(data, &reply); err != nil {
		return "", fmt.Errorf("parse registration reply: %w", err)
	}
	if !protocol.ValidNodeID(reply.ID) {
		return "", fmt.Errorf("registration reply carries an invalid ID %q", reply.ID)
	}
	return reply.ID, nil
}

// RegisterLoop retries registration with backoff until it succeeds or ctx
// ends. The node keeps running locally in the meantime.
func RegisterLoop(ctx context.Context, server string, id *identity.Identity, log *slog.Logger) (string, error) {
	delay := backoffMin
	for {
		nodeID, err := Register(ctx, server, id)
		if err == nil {
			return nodeID, nil
		}
		log.Warn("registration failed, will retry", "error", err, "retry_in", delay)
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(jitter(delay)):
		}
		delay = min(delay*2, backoffMax)
	}
}

// SignalSender sends one signaling message to the service over the
// presence connection.
type SignalSender func(protocol.SignalMessage) error

// VisitHandler answers visit offers relayed by the service. HandleVisitOffer
// must validate its input and return quickly; the slow work happens on its
// own goroutines.
type VisitHandler interface {
	HandleVisitOffer(ctx context.Context, visitID, sdp, from string, send SignalSender)
}

// PresenceLoop keeps one authenticated presence connection open, reconnecting
// with exponential backoff and jitter until ctx ends. Failures never affect
// the local preview. Visit offers arriving over the connection go to visits;
// a nil handler drops them. It returns ErrInUse, without reconnecting,
// when the service refuses it because another copy of the node runs.
func PresenceLoop(ctx context.Context, server string, id *identity.Identity, visits VisitHandler, log *slog.Logger) error {
	// A fresh start is "connecting", not "unreachable", until an attempt
	// actually fails; so is the time after the loop stops.
	presenceFailing.Store(false)
	defer presenceFailing.Store(false)
	delay := backoffMin
	for {
		err := runPresence(ctx, server, id, visits, log, func() {
			delay = backoffMin
			presenceFailing.Store(false)
		})
		if ctx.Err() != nil {
			return ctx.Err()
		}
		presenceFailing.Store(true)
		if isInUse(err) {
			log.Warn("another copy of this node is already running on another computer; this one stays offline")
			return ErrInUse
		}
		log.Warn("presence connection lost, will reconnect", "error", err, "retry_in", delay)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(jitter(delay)):
		}
		delay = min(delay*2, backoffMax)
	}
}

// presenceFailing is set while the presence loop's latest attempt to
// reach the service failed, so the owner can tell "still connecting" from
// "the service cannot be reached".
var presenceFailing atomic.Bool

// PresenceFailing reports whether the latest attempt to reach the service
// failed (false while connected, while still trying for the first time,
// and while not publishing).
func PresenceFailing() bool { return presenceFailing.Load() }

// runPresence dials, authenticates and holds one presence connection. It
// calls connected once authentication succeeds.
func runPresence(ctx context.Context, server string, id *identity.Identity, visits VisitHandler, log *slog.Logger, connected func()) error {
	wsURL, err := presenceURL(server, id.NodeID)
	if err != nil {
		return err
	}
	dialCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	c, _, err := websocket.Dial(dialCtx, wsURL, nil)
	cancel()
	if err != nil {
		return err
	}
	defer c.CloseNow()
	// The presence socket also carries signaling (relayed SDP).
	c.SetReadLimit(protocol.MaxSignalMessageBytes)

	if err := answerChallenge(ctx, c, id); err != nil {
		return err
	}
	connected()
	log.Info("connected to server", "server", server)

	// send is scoped to this connection; concurrent writes are safe with
	// coder/websocket.
	send := SignalSender(func(m protocol.SignalMessage) error {
		data, err := m.Encode()
		if err != nil {
			return err
		}
		wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		return c.Write(wctx, websocket.MessageText, data)
	})

	// A handler that wants to send on its own (the port-mapping probe) gets
	// this connection's sender for as long as the connection lives.
	if ss, ok := visits.(interface{ SetSender(SignalSender) }); ok {
		ss.SetSender(send)
		defer ss.SetSender(nil)
	}

	// Read loop: answers server pings and dispatches relayed visit offers.
	// Every incoming message is hostile input until validated; anything
	// unexpected is dropped, never acted on.
	readErr := make(chan error, 1)
	go func() {
		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				readErr <- err
				return
			}
			msg, err := protocol.DecodeSignalMessage(data)
			if err != nil || msg.V != protocol.SignalVersion {
				log.Warn("dropping unexpected message from server")
				continue
			}
			switch msg.Type {
			case protocol.SignalVisitOffer:
				if visits != nil {
					visits.HandleVisitOffer(ctx, msg.Visit, msg.SDP, msg.From, send)
				}
			case protocol.SignalAuthCheck:
				// The service asks before mailing a sign-in code: only
				// addresses on a protected folder get one. The answer
				// carries nothing but yes or no.
				ac, ok := visits.(AuthChecker)
				if !ok || !protocol.ValidVisitID(msg.Visit) {
					continue
				}
				reply := protocol.SignalAuthDeny
				if ac.AuthCheck(msg.Email) {
					reply = protocol.SignalAuthAllow
				}
				if err := send(protocol.SignalMessage{V: protocol.SignalVersion, Type: reply, Visit: msg.Visit}); err != nil {
					log.Warn("could not answer a sign-in check", "error", err)
				}
			case protocol.SignalProbeSent:
				// Informational; the probe itself is observed on the UDP
				// socket. Nothing to do.
			default:
				log.Warn("dropping unexpected message from server")
			}
		}
	}()

	ticker := time.NewTicker(protocol.PingInterval)
	defer ticker.Stop()
	for {
		select {
		case err := <-readErr:
			return err
		case <-ctx.Done():
			c.Close(websocket.StatusNormalClosure, "shutting down")
			return ctx.Err()
		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, protocol.IdleTimeout-protocol.PingInterval)
			err := c.Ping(pingCtx)
			cancel()
			if err != nil {
				return err
			}
		}
	}
}

// answerChallenge performs the node side of the challenge handshake.
func answerChallenge(ctx context.Context, c *websocket.Conn, id *identity.Identity) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	_, data, err := c.Read(ctx)
	if err != nil {
		return fmt.Errorf("read challenge: %w", err)
	}
	msg, err := protocol.DecodePresenceMessage(data)
	if err != nil || msg.Type != "challenge" {
		return errors.New("expected a challenge")
	}
	challenge, err := base64.StdEncoding.DecodeString(msg.Challenge)
	if err != nil || len(challenge) != protocol.ChallengeSize {
		return errors.New("malformed challenge")
	}

	sig := id.Sign(protocol.PresenceSigningBytes(challenge, id.NodeID))
	reply, err := protocol.PresenceMessage{
		Type:      "auth",
		Signature: base64.StdEncoding.EncodeToString(sig),
	}.Encode()
	if err != nil {
		return err
	}
	if err := c.Write(ctx, websocket.MessageText, reply); err != nil {
		return fmt.Errorf("send auth: %w", err)
	}

	_, data, err = c.Read(ctx)
	if err != nil {
		return fmt.Errorf("read auth result: %w", err)
	}
	ok, err := protocol.DecodePresenceMessage(data)
	if err != nil || ok.Type != "ok" {
		return errors.New("authentication rejected")
	}
	return nil
}

// presenceURL converts the service base URL into the WebSocket connect URL.
func presenceURL(server, nodeID string) (string, error) {
	u, err := url.Parse(server)
	if err != nil {
		return "", fmt.Errorf("invalid server URL %q: %w", server, err)
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	default:
		return "", fmt.Errorf("invalid server URL scheme %q", u.Scheme)
	}
	u.Path = strings.TrimSuffix(u.Path, "/") + "/v1/node/connect"
	u.RawQuery = url.Values{"id": {nodeID}}.Encode()
	return u.String(), nil
}

// Status fetches the server's view of the node.
func Status(ctx context.Context, server, nodeID string) (protocol.NodeStatus, error) {
	var st protocol.NodeStatus
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(server, "/")+"/v1/nodes/"+url.PathEscape(nodeID), nil)
	if err != nil {
		return st, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return st, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return st, fmt.Errorf("server replied %s", resp.Status)
	}
	err = json.NewDecoder(io.LimitReader(resp.Body, protocol.MaxBodyBytes)).Decode(&st)
	return st, err
}

// jitter spreads reconnects out by up to half the base delay.
func jitter(d time.Duration) time.Duration {
	return d + rand.N(d/2+1)
}
