// Package devsim simulates an eSSL/ZKTeco ADMS device: the handshake, heartbeats, punch uploads,
// command execution and an offline backlog. It speaks the same requests as the real terminal
// (see docs/adms-protocol.md), so it exercises the service end to end without hardware.
package devsim

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Verify codes, as the service maps them.
const (
	VerifyPassword    = 0
	VerifyFingerprint = 1
	VerifyCard        = 2
	VerifyFace        = 15
)

const deviceTimeLayout = "2006-01-02 15:04:05"

// Punch is one stored attendance record.
type Punch struct {
	Pin    string
	At     time.Time
	Verify int
}

func (p Punch) line(loc *time.Location) string {
	return fmt.Sprintf("%s\t%s\t0\t%d\t0\t0\t0", p.Pin, p.At.In(loc).Format(deviceTimeLayout), p.Verify)
}

// Device is one simulated terminal. It is safe for concurrent use.
type Device struct {
	Server   string // base URL, e.g. http://localhost:8001
	Serial   string
	Location *time.Location
	Client   *http.Client

	// OnCommand is called for every command received, before it is acknowledged; it returns
	// the Return code to report (0 success, negative failure). Nil acknowledges everything.
	OnCommand func(id int64, command string) int

	// Enforce makes the device behave like a real terminal's user list: a punch for a pin it
	// was not given (or that was deleted) is refused on the device and never uploaded.
	Enforce bool

	mu        sync.Mutex
	users     map[string]string   // pin → name, from DATA UPDATE/DELETE USERINFO
	templates map[string][]string // pin → template lines the device holds
	online    bool
	stamp     int
	punches   []Punch // everything ever recorded, like the device's own log
	unsent    []Punch // recorded but not yet acknowledged by the server
	executed  []string
}

func New(server string, serial string) *Device {
	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		loc = time.FixedZone("IST", 5*3600+1800)
	}
	return &Device{Server: strings.TrimRight(server, "/"), Serial: serial, Location: loc, Client: &http.Client{Timeout: 10 * time.Second},
		online: true, users: map[string]string{}, templates: map[string][]string{}}
}

func (d *Device) url(path string, query url.Values) string {
	query.Set("SN", d.Serial)
	return d.Server + path + "?" + query.Encode()
}

func (d *Device) do(method string, path string, query url.Values, body string) (int, string, error) {
	req, err := http.NewRequest(method, d.url(path, query), strings.NewReader(body))
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Content-Type", "text/plain")
	resp, err := d.Client.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	text, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(text), nil
}

// Handshake does the boot-time options request and resumes from the ATTLOG stamp the server
// returns, as the real device does.
func (d *Device) Handshake() (string, error) {
	code, text, err := d.do(http.MethodGet, "/iclock/cdata", url.Values{"options": {"all"}, "pushver": {"2.4.1"}, "language": {"69"}}, "")
	if err != nil {
		return "", err
	}
	if code != http.StatusOK {
		return text, fmt.Errorf("handshake: HTTP %d: %s", code, strings.TrimSpace(text))
	}

	for _, line := range strings.Split(text, "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), "ATTLOGStamp="); ok {
			if n, err := strconv.Atoi(value); err == nil {
				d.mu.Lock()
				if n > d.stamp {
					d.stamp = n
				}
				d.mu.Unlock()
			}
		}
	}
	return text, nil
}

// SetOnline simulates the network dropping or returning. Offline punches are kept and sent on
// the next Flush after coming back.
func (d *Device) SetOnline(online bool) {
	d.mu.Lock()
	d.online = online
	d.mu.Unlock()
}

// Punch records a punch now and, when online, uploads it straight away (Realtime=1).
func (d *Device) Punch(pin string, verify int) error {
	return d.PunchAt(pin, verify, time.Now())
}

// ErrRefused is returned for a punch an enforcing device turns away.
var ErrRefused = fmt.Errorf("refused at the device: user not enrolled")

func (d *Device) PunchAt(pin string, verify int, at time.Time) error {
	d.mu.Lock()
	if d.Enforce {
		if _, ok := d.users[pin]; !ok {
			d.mu.Unlock()
			return ErrRefused
		}
	}
	punch := Punch{Pin: pin, At: at.Truncate(time.Second), Verify: verify}
	d.punches = append(d.punches, punch)
	d.unsent = append(d.unsent, punch)
	online := d.online
	d.mu.Unlock()

	if !online {
		return nil
	}
	return d.Flush()
}

// Flush uploads every unacknowledged punch in one ATTLOG batch. On any failure the punches stay
// queued, exactly the behaviour the service relies on.
func (d *Device) Flush() error {
	d.mu.Lock()
	if !d.online || len(d.unsent) == 0 {
		d.mu.Unlock()
		return nil
	}
	batch := append([]Punch(nil), d.unsent...)
	stamp := d.stamp + 1
	d.mu.Unlock()

	if err := d.upload(batch, stamp); err != nil {
		return err
	}

	d.mu.Lock()
	d.unsent = d.unsent[len(batch):]
	d.stamp = stamp
	d.mu.Unlock()
	return nil
}

func (d *Device) upload(batch []Punch, stamp int) error {
	lines := make([]string, len(batch))
	for i, p := range batch {
		lines[i] = p.line(d.Location)
	}

	code, text, err := d.do(http.MethodPost, "/iclock/cdata", url.Values{"table": {"ATTLOG"}, "Stamp": {strconv.Itoa(stamp)}}, strings.Join(lines, "\n")+"\n")
	if err != nil {
		return err
	}
	if code != http.StatusOK || !strings.HasPrefix(text, "OK") {
		return fmt.Errorf("ATTLOG upload: HTTP %d: %s", code, strings.TrimSpace(text))
	}
	return nil
}

// Poll is one heartbeat: fetch queued commands, run them, report the results. It returns the
// commands received.
func (d *Device) Poll() ([]string, error) {
	d.mu.Lock()
	online := d.online
	d.mu.Unlock()
	if !online {
		return nil, nil
	}

	code, text, err := d.do(http.MethodGet, "/iclock/getrequest", url.Values{}, "")
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("getrequest: HTTP %d: %s", code, strings.TrimSpace(text))
	}

	var received []string
	var results []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		rest, ok := strings.CutPrefix(line, "C:")
		if !ok {
			continue
		}
		idText, command, ok := strings.Cut(rest, ":")
		id, err := strconv.ParseInt(idText, 10, 64)
		if !ok || err != nil {
			continue
		}
		received = append(received, command)
		results = append(results, d.execute(id, command))
	}

	if len(results) > 0 {
		code, text, err := d.do(http.MethodPost, "/iclock/devicecmd", url.Values{}, strings.Join(results, "\n")+"\n")
		if err != nil {
			return received, err
		}
		if code != http.StatusOK {
			return received, fmt.Errorf("devicecmd: HTTP %d: %s", code, strings.TrimSpace(text))
		}
	}

	return received, nil
}

// execute runs one command. DATA QUERY ATTLOG re-sends stored punches in its window, the way
// a device answers a re-pull; everything else is just acknowledged.
func (d *Device) execute(id int64, command string) string {
	ret := 0
	if d.OnCommand != nil {
		ret = d.OnCommand(id, command)
	}

	d.mu.Lock()
	d.executed = append(d.executed, command)
	d.mu.Unlock()

	if ret == 0 {
		d.applyUserCommand(command)
	}

	if ret == 0 && strings.HasPrefix(command, "DATA QUERY ATTLOG") {
		if err := d.resendWindow(command); err != nil {
			ret = -1
		}
	}

	verb, _, _ := strings.Cut(command, " ")
	return fmt.Sprintf("ID=%d&Return=%d&CMD=%s", id, ret, verb)
}

func (d *Device) resendWindow(command string) error {
	start, end, err := parseQueryWindow(command, d.Location)
	if err != nil {
		return err
	}

	d.mu.Lock()
	var window []Punch
	for _, p := range d.punches {
		if !p.At.Before(start) && !p.At.After(end) {
			window = append(window, p)
		}
	}
	stamp := d.stamp + 1
	d.mu.Unlock()

	if len(window) == 0 {
		return nil
	}
	if err := d.upload(window, stamp); err != nil {
		return err
	}
	d.mu.Lock()
	d.stamp = stamp
	d.mu.Unlock()
	return nil
}

// applyUserCommand keeps the device's user list as the real one would.
func (d *Device) applyUserCommand(command string) {
	fields := map[string]string{}
	verb, rest, _ := strings.Cut(command, " ")
	table, rest, _ := strings.Cut(rest, " ")
	kind, rest, _ := strings.Cut(rest, " ")
	for _, field := range strings.Split(rest, "\t") {
		if key, value, ok := strings.Cut(field, "="); ok {
			fields[strings.ToUpper(key)] = value
		}
	}
	if verb != "DATA" {
		return
	}
	pin := fields["PIN"]

	d.mu.Lock()
	defer d.mu.Unlock()
	switch {
	case table == "UPDATE" && kind == "USERINFO":
		d.users[pin] = fields["NAME"]
	case table == "DELETE" && kind == "USERINFO":
		delete(d.users, pin)
		delete(d.templates, pin)
	case table == "UPDATE":
		d.templates[pin] = append(d.templates[pin], kind+" "+rest)
	}
}

// Enroll simulates enrolling a face at the device: the user must already be on it, and the
// template is uploaded in OPERLOG as a real terminal does.
func (d *Device) Enroll(pin string) error {
	d.mu.Lock()
	_, ok := d.users[pin]
	d.mu.Unlock()
	if !ok {
		return fmt.Errorf("pin %s is not on the device yet; add the member in the portal first", pin)
	}

	line := fmt.Sprintf("FACE PIN=%s\tFID=0\tSIZE=16\tVALID=1\tTMP=U0lNVUxBVEVEX0ZBQ0U=", pin)
	code, text, err := d.do(http.MethodPost, "/iclock/cdata", url.Values{"table": {"OPERLOG"}, "Stamp": {"1"}}, line+"\n")
	if err != nil {
		return err
	}
	if code != http.StatusOK {
		return fmt.Errorf("OPERLOG upload: HTTP %d: %s", code, strings.TrimSpace(text))
	}

	d.mu.Lock()
	d.templates[pin] = append(d.templates[pin], line)
	d.mu.Unlock()
	return nil
}

// Users returns the pins currently on the device.
func (d *Device) Users() map[string]string {
	d.mu.Lock()
	defer d.mu.Unlock()
	users := map[string]string{}
	for pin, name := range d.users {
		users[pin] = name
	}
	return users
}

// Templates returns the template lines the device holds for a pin.
func (d *Device) Templates(pin string) []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.templates[pin]...)
}

func parseQueryWindow(command string, loc *time.Location) (time.Time, time.Time, error) {
	var start, end time.Time
	for _, field := range strings.Split(strings.TrimPrefix(command, "DATA QUERY ATTLOG "), "\t") {
		key, value, _ := strings.Cut(field, "=")
		t, err := time.ParseInLocation(deviceTimeLayout, value, loc)
		if err != nil {
			return start, end, fmt.Errorf("bad %s: %w", key, err)
		}
		switch key {
		case "StartTime":
			start = t
		case "EndTime":
			end = t
		}
	}
	return start, end, nil
}

// Executed lists every command the device has run, for tests and the CLI.
func (d *Device) Executed() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.executed...)
}

// Unsent is how many punches are waiting for the server.
func (d *Device) Unsent() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.unsent)
}
