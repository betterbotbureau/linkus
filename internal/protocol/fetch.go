package protocol

import "encoding/json"

// The linkus-fetch-v1 protocol: one WebRTC data channel, one request at a
// time. The visitor sends a text "get" message; the node answers with a
// text "head", binary chunks in order, and a text "done" — or a text
// "error" on any failure.
const (
	// FetchProtocol is the data channel label/protocol name.
	FetchProtocol = "linkus-fetch-v1"
	// MaxFetchRequestBytes caps one request message. Larger requests close
	// the channel.
	MaxFetchRequestBytes = 4096
	// FetchChunkSize is the largest binary chunk the node sends.
	FetchChunkSize = 16384
	// MaxChannelsPerVisit caps data channels on one visit. The bootstrap
	// shell opens a small pool for parallel fetches; the node closes
	// anything beyond this.
	MaxChannelsPerVisit = 8
)

// Fetch message types.
const (
	FetchGet   = "get"
	FetchHead  = "head"
	FetchDone  = "done"
	FetchError = "error"
)

// FetchRequest is the visitor's text request for one file.
type FetchRequest struct {
	V    int    `json:"v"`
	Type string `json:"type"` // "get"
	Path string `json:"path"`
}

// FetchHeadMessage precedes the body chunks of a successful fetch. Status
// is always 200; failures use FetchErrorMessage instead, so a visitor
// learns no more than a preview request would tell it.
type FetchHeadMessage struct {
	V           int    `json:"v"`
	Type        string `json:"type"` // "head"
	Status      int    `json:"status"`
	Path        string `json:"path"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
}

// FetchDoneMessage follows the last body chunk.
type FetchDoneMessage struct {
	V    int    `json:"v"`
	Type string `json:"type"` // "done"
}

// FetchErrorMessage reports a failed request. Status is 404 for everything
// that does not resolve to a servable file.
type FetchErrorMessage struct {
	V      int    `json:"v"`
	Type   string `json:"type"` // "error"
	Status int    `json:"status"`
}

// Encode marshals a fetch message for the wire.
func (m FetchHeadMessage) Encode() ([]byte, error)  { return json.Marshal(m) }
func (m FetchDoneMessage) Encode() ([]byte, error)  { return json.Marshal(m) }
func (m FetchErrorMessage) Encode() ([]byte, error) { return json.Marshal(m) }
func (m FetchRequest) Encode() ([]byte, error)      { return json.Marshal(m) }

// DecodeFetchRequest parses a request message, rejecting oversized input.
func DecodeFetchRequest(data []byte) (FetchRequest, error) {
	var m FetchRequest
	if len(data) > MaxFetchRequestBytes {
		return m, ErrMessageTooLarge
	}
	err := json.Unmarshal(data, &m)
	return m, err
}
