package portmap

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/huin/goupnp/dcps/internetgateway2"
)

// fakeIGD is an HTTP server speaking just enough UPnP IGD for goupnp's
// typed clients: a root device description and a SOAP control endpoint.
type fakeIGD struct {
	ts *httptest.Server

	mu        sync.Mutex
	added     []string   // AddPortMapping request bodies
	deleted   []string   // DeletePortMapping request bodies
	addFaults []string   // queued UPnP error codes for AddPortMapping calls
	entries   []igdEntry // the router's mapping table
}

// igdEntry is one row of the fake router's mapping table.
type igdEntry struct {
	extPort   uint16
	intPort   uint16
	intClient string
	desc      string
}

const igdRootDesc = `<?xml version="1.0"?>
<root xmlns="urn:schemas-upnp-org:device-1-0">
  <specVersion><major>1</major><minor>0</minor></specVersion>
  <device>
    <deviceType>urn:schemas-upnp-org:device:InternetGatewayDevice:1</deviceType>
    <friendlyName>Fake IGD</friendlyName>
    <manufacturer>linkus tests</manufacturer>
    <modelName>fake</modelName>
    <UDN>uuid:linkus-fake-igd</UDN>
    <serviceList>
      <service>
        <serviceType>urn:schemas-upnp-org:service:WANIPConnection:1</serviceType>
        <serviceId>urn:upnp-org:serviceId:WANIPConn1</serviceId>
        <SCPDURL>/scpd.xml</SCPDURL>
        <controlURL>/control</controlURL>
        <eventSubURL>/events</eventSubURL>
      </service>
    </serviceList>
  </device>
</root>`

func soapEnvelope(body string) string {
	return `<?xml version="1.0"?>` +
		`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"` +
		` s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/">` +
		`<s:Body>` + body + `</s:Body></s:Envelope>`
}

func startFakeIGD(t *testing.T) *fakeIGD {
	t.Helper()
	g := &fakeIGD{}
	mux := http.NewServeMux()
	mux.HandleFunc("/rootDesc.xml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		io.WriteString(w, igdRootDesc) //nolint:errcheck // test fake
	})
	mux.HandleFunc("/control", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		action := r.Header.Get("SOAPACTION")
		w.Header().Set("Content-Type", "text/xml")
		g.mu.Lock()
		defer g.mu.Unlock()
		fault := func(code, text string) {
			w.WriteHeader(http.StatusInternalServerError)
			io.WriteString(w, soapEnvelope(`<s:Fault>`+ //nolint:errcheck
				`<faultcode>s:Client</faultcode><faultstring>UPnPError</faultstring>`+
				`<detail><UPnPError xmlns="urn:schemas-upnp-org:control-1-0">`+
				`<errorCode>`+code+`</errorCode><errorDescription>`+text+`</errorDescription>`+
				`</UPnPError></detail></s:Fault>`))
		}
		switch {
		case strings.Contains(action, "AddPortMapping"):
			if len(g.addFaults) > 0 {
				code := g.addFaults[0]
				g.addFaults = g.addFaults[1:]
				fault(code, "simulated "+code)
				return
			}
			g.added = append(g.added, string(body))
			io.WriteString(w, soapEnvelope( //nolint:errcheck
				`<u:AddPortMappingResponse xmlns:u="urn:schemas-upnp-org:service:WANIPConnection:1"/>`))
		case strings.Contains(action, "GetGenericPortMappingEntry"):
			var idx int
			if _, err := fmt.Sscanf(between(string(body), "<NewPortMappingIndex>", "</NewPortMappingIndex>"), "%d", &idx); err != nil || idx >= len(g.entries) {
				fault("713", "SpecifiedArrayIndexInvalid")
				return
			}
			e := g.entries[idx]
			io.WriteString(w, soapEnvelope(fmt.Sprintf( //nolint:errcheck
				`<u:GetGenericPortMappingEntryResponse xmlns:u="urn:schemas-upnp-org:service:WANIPConnection:1">`+
					`<NewRemoteHost></NewRemoteHost><NewExternalPort>%d</NewExternalPort>`+
					`<NewProtocol>UDP</NewProtocol><NewInternalPort>%d</NewInternalPort>`+
					`<NewInternalClient>%s</NewInternalClient><NewEnabled>1</NewEnabled>`+
					`<NewPortMappingDescription>%s</NewPortMappingDescription>`+
					`<NewLeaseDuration>0</NewLeaseDuration>`+
					`</u:GetGenericPortMappingEntryResponse>`,
				e.extPort, e.intPort, e.intClient, e.desc)))
		case strings.Contains(action, "GetExternalIPAddress"):
			io.WriteString(w, soapEnvelope( //nolint:errcheck
				`<u:GetExternalIPAddressResponse xmlns:u="urn:schemas-upnp-org:service:WANIPConnection:1">`+
					`<NewExternalIPAddress>203.0.113.77</NewExternalIPAddress>`+
					`</u:GetExternalIPAddressResponse>`))
		case strings.Contains(action, "DeletePortMapping"):
			g.deleted = append(g.deleted, string(body))
			io.WriteString(w, soapEnvelope( //nolint:errcheck
				`<u:DeletePortMappingResponse xmlns:u="urn:schemas-upnp-org:service:WANIPConnection:1"/>`))
		default:
			t.Errorf("unexpected SOAP action %q", action)
			w.WriteHeader(http.StatusInternalServerError)
		}
	})
	g.ts = httptest.NewServer(mux)
	t.Cleanup(g.ts.Close)
	return g
}

func (g *fakeIGD) client(t *testing.T) igdClient {
	t.Helper()
	loc, err := url.Parse(g.ts.URL + "/rootDesc.xml")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	clients, err := internetgateway2.NewWANIPConnection1ClientsByURLCtx(ctx, loc)
	if err != nil || len(clients) == 0 {
		t.Fatalf("no clients from fake IGD: %v", err)
	}
	return clients[0]
}

func TestUPnPMapAndDelete(t *testing.T) {
	g := startFakeIGD(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	m, err := upnpMapWith(ctx, g.client(t), netip.MustParseAddr("192.168.1.10"), 61000, testLog())
	if err != nil {
		t.Fatal(err)
	}
	if m.Protocol != "upnp" || m.ExternalIP != netip.MustParseAddr("203.0.113.77") ||
		m.ExternalPort != 61000 || m.InternalPort != 61000 {
		t.Fatalf("mapping = %s", m)
	}

	g.mu.Lock()
	if len(g.added) != 1 || !strings.Contains(g.added[0], "<NewInternalClient>192.168.1.10</NewInternalClient>") ||
		!strings.Contains(g.added[0], fmt.Sprintf("<NewExternalPort>%d</NewExternalPort>", 61000)) ||
		!strings.Contains(g.added[0], "<NewProtocol>UDP</NewProtocol>") {
		t.Fatalf("AddPortMapping bodies: %q", g.added)
	}
	g.mu.Unlock()

	if err := m.Delete(ctx); err != nil {
		t.Fatal(err)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.deleted) != 1 || !strings.Contains(g.deleted[0], fmt.Sprintf("<NewExternalPort>%d</NewExternalPort>", 61000)) {
		t.Fatalf("DeletePortMapping bodies: %q", g.deleted)
	}
}

// between returns the text between the first markers, or "".
func between(s, a, b string) string {
	_, rest, ok := strings.Cut(s, a)
	if !ok {
		return ""
	}
	out, _, ok := strings.Cut(rest, b)
	if !ok {
		return ""
	}
	return out
}

func TestUPnPPermanentLeaseFallback(t *testing.T) {
	g := startFakeIGD(t)
	g.addFaults = []string{"725"} // first attempt faults with 725
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	m, err := upnpMapWith(ctx, g.client(t), netip.MustParseAddr("192.168.1.10"), 61000, testLog())
	if err != nil {
		t.Fatal(err)
	}
	if m.Protocol != "upnp" {
		t.Fatalf("mapping = %s", m)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.added) != 1 || !strings.Contains(g.added[0], "<NewLeaseDuration>0</NewLeaseDuration>") {
		t.Fatalf("expected a permanent-lease retry, got %q", g.added)
	}
}

// TestUPnPFullTableCleanup reproduces the field failure "Port mapping table
// id full !": AddPortMapping faults, and recovery deletes exactly the stale
// entries of this node on this machine — never other devices' or programs'
// — then retries.
func TestUPnPFullTableCleanup(t *testing.T) {
	g := startFakeIGD(t)
	g.addFaults = []string{"501"} // the router's "table full" complaint
	g.entries = []igdEntry{
		{extPort: 50000, intPort: 50000, intClient: "192.168.1.10", desc: "Link Node"}, // ours, stale
		{extPort: 50001, intPort: 50001, intClient: "192.168.1.99", desc: "Link Node"}, // another machine's node
		{extPort: 50002, intPort: 50002, intClient: "192.168.1.10", desc: "Other App"}, // another program
		{extPort: 61000, intPort: 50003, intClient: "192.168.1.10", desc: "Link Node"}, // ours, conflicts with the wanted port
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	m, err := upnpMapWith(ctx, g.client(t), netip.MustParseAddr("192.168.1.10"), 61000, testLog())
	if err != nil {
		t.Fatal(err)
	}
	if m.ExternalPort != 61000 {
		t.Fatalf("mapping = %s", m)
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.added) != 1 {
		t.Fatalf("AddPortMapping succeeded %d times, want 1 (after cleanup)", len(g.added))
	}
	if len(g.deleted) != 2 {
		t.Fatalf("deleted %d entries, want exactly our 2 stale ones: %q", len(g.deleted), g.deleted)
	}
	for _, d := range g.deleted {
		if !strings.Contains(d, "<NewExternalPort>50000</NewExternalPort>") &&
			!strings.Contains(d, "<NewExternalPort>61000</NewExternalPort>") {
			t.Fatalf("deleted a foreign entry: %q", d)
		}
	}
}

// TestUPnPNoStaleEntriesMeansNoRetry: a failure with nothing of ours in the
// table is reported as-is.
func TestUPnPNoStaleEntriesMeansNoRetry(t *testing.T) {
	g := startFakeIGD(t)
	g.addFaults = []string{"501", "501"}
	g.entries = []igdEntry{
		{extPort: 50001, intPort: 50001, intClient: "192.168.1.99", desc: "Link Node"},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := upnpMapWith(ctx, g.client(t), netip.MustParseAddr("192.168.1.10"), 61000, testLog())
	if err == nil || !strings.Contains(err.Error(), "501") {
		t.Fatalf("err = %v, want the 501 fault", err)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.deleted) != 0 {
		t.Fatalf("deleted foreign entries: %q", g.deleted)
	}
}

func testLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// TestUPnPTableFullMessageIsActionable: when nothing of ours can be cleaned
// and the router says its table is full, the error tells the owner what to
// do about the router.
func TestUPnPTableFullMessageIsActionable(t *testing.T) {
	g := startFakeIGD(t)
	g.addFaults = []string{"728", "728"}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := upnpMapWith(ctx, g.client(t), netip.MustParseAddr("192.168.1.10"), 61000, testLog())
	if err == nil || !strings.Contains(err.Error(), "clear the router's UPnP port mapping list") {
		t.Fatalf("err = %v, want the actionable table-full message", err)
	}
}
