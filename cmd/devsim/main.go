// Command devsim runs simulated ADMS devices against essl_integration_service.
//
// Interactive, one device:
//
//	go run ./cmd/devsim -server http://localhost:8001 -sn DEVSIM0001
//
// then type: punch <pin> [face|finger|card|password] · offline · online · status · quit
//
// Load test, many devices (registered through the portal API with a gym owner's login):
//
//	DEVSIM_EMAIL=owner@... DEVSIM_PASSWORD=... go run ./cmd/devsim -load 50 -register -duration 2m
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/http/cookiejar"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"essl_integration_service/devsim"
)

func main() {
	server := flag.String("server", "http://localhost:8001", "service base URL (the /iclock endpoint lives at the root)")
	serial := flag.String("sn", "DEVSIM0001", "serial number of the simulated device")
	heartbeat := flag.Duration("heartbeat", 10*time.Second, "how often to poll for commands")
	load := flag.Int("load", 0, "run this many devices in load mode instead of one interactive device")
	prefix := flag.String("prefix", "LOADSIM", "serial prefix for load-mode devices")
	register := flag.Bool("register", false, "load mode: register the devices first, using DEVSIM_EMAIL / DEVSIM_PASSWORD")
	duration := flag.Duration("duration", time.Minute, "load mode: how long to run")
	punchEvery := flag.Duration("punch-every", 30*time.Second, "load mode: average gap between punches per device")
	maxPin := flag.Int("max-pin", 50, "load mode: punches use pins 1..max-pin")
	flag.Parse()

	if *load > 0 {
		os.Exit(runLoad(*server, *prefix, *load, *register, *heartbeat, *duration, *punchEvery, *maxPin))
	}
	os.Exit(runInteractive(*server, *serial, *heartbeat))
}

var verifyNames = map[string]int{
	"face": devsim.VerifyFace, "finger": devsim.VerifyFingerprint, "fp": devsim.VerifyFingerprint,
	"card": devsim.VerifyCard, "password": devsim.VerifyPassword, "pw": devsim.VerifyPassword,
}

func runInteractive(server string, serial string, heartbeat time.Duration) int {
	device := devsim.New(server, serial)
	device.OnCommand = func(id int64, command string) int {
		fmt.Printf("\n<- command %d: %s\n> ", id, command)
		return 0
	}

	if _, err := device.Handshake(); err != nil {
		fmt.Fprintln(os.Stderr, "handshake failed:", err)
		fmt.Fprintln(os.Stderr, "Is the serial registered in the portal (Settings → Devices)?")
		return 1
	}
	fmt.Printf("%s connected to %s; polling every %s\n", serial, server, heartbeat)
	fmt.Println("commands: punch <pin> [face|finger|card|password] · offline · online · status · quit")

	go func() {
		for range time.Tick(heartbeat) {
			if _, err := device.Poll(); err != nil {
				fmt.Printf("\npoll failed: %v\n> ", err)
			}
		}
	}()

	scanner := bufio.NewScanner(os.Stdin)
	for fmt.Print("> "); scanner.Scan(); fmt.Print("> ") {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "punch":
			if len(fields) < 2 {
				fmt.Println("usage: punch <pin> [face|finger|card|password]")
				continue
			}
			verify := devsim.VerifyFace
			if len(fields) > 2 {
				v, ok := verifyNames[fields[2]]
				if !ok {
					fmt.Println("verify must be face, finger, card or password")
					continue
				}
				verify = v
			}
			if err := device.Punch(fields[1], verify); err != nil {
				fmt.Println("upload failed, kept for retry:", err)
				continue
			}
			if device.Unsent() > 0 {
				fmt.Printf("stored offline (%d waiting)\n", device.Unsent())
			} else {
				fmt.Println("sent")
			}
		case "offline":
			device.SetOnline(false)
			fmt.Println("offline: punches are stored on the device")
		case "online":
			device.SetOnline(true)
			if err := device.Flush(); err != nil {
				fmt.Println("backlog upload failed:", err)
				continue
			}
			fmt.Println("online: backlog sent")
		case "status":
			fmt.Printf("waiting to send: %d · commands run: %d\n", device.Unsent(), len(device.Executed()))
		case "quit", "exit":
			return 0
		default:
			fmt.Println("unknown command")
		}
	}
	return 0
}

// ---- load mode ----

type latencies struct {
	mu      sync.Mutex
	samples map[string][]time.Duration
	errors  map[string]int
}

func (l *latencies) record(kind string, took time.Duration, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err != nil {
		l.errors[kind]++
		return
	}
	l.samples[kind] = append(l.samples[kind], took)
}

func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	return sorted[min(len(sorted)-1, int(float64(len(sorted))*p))]
}

func runLoad(server string, prefix string, count int, register bool, heartbeat time.Duration, duration time.Duration, punchEvery time.Duration, maxPin int) int {
	serials := make([]string, count)
	for i := range serials {
		serials[i] = fmt.Sprintf("%s%04d", prefix, i+1)
	}

	if register {
		if err := registerDevices(server, serials); err != nil {
			fmt.Fprintln(os.Stderr, "register:", err)
			return 1
		}
	}

	stats := &latencies{samples: map[string][]time.Duration{}, errors: map[string]int{}}
	deadline := time.Now().Add(duration)
	var wg sync.WaitGroup

	for _, serial := range serials {
		wg.Add(1)
		go func(serial string) {
			defer wg.Done()
			device := devsim.New(server, serial)

			start := time.Now()
			_, err := device.Handshake()
			stats.record("handshake", time.Since(start), err)
			if err != nil {
				return
			}

			// Stagger devices so they do not all poll in the same instant.
			time.Sleep(time.Duration(rand.Int64N(int64(heartbeat))))
			nextPoll := time.Now()
			nextPunch := time.Now().Add(time.Duration(rand.Int64N(int64(punchEvery) * 2)))

			for time.Now().Before(deadline) {
				now := time.Now()
				if !now.Before(nextPoll) {
					start := time.Now()
					_, err := device.Poll()
					stats.record("poll", time.Since(start), err)
					nextPoll = now.Add(heartbeat)
				}
				if !now.Before(nextPunch) {
					start := time.Now()
					err := device.Punch(strconv.Itoa(1+rand.IntN(maxPin)), devsim.VerifyFace)
					stats.record("punch", time.Since(start), err)
					nextPunch = now.Add(time.Duration(rand.Int64N(int64(punchEvery) * 2)))
				}
				time.Sleep(100 * time.Millisecond)
			}
		}(serial)
	}

	fmt.Printf("running %d devices for %s (heartbeat %s, a punch every ~%s each)\n", count, duration, heartbeat, punchEvery)
	wg.Wait()

	failed := false
	for _, kind := range []string{"handshake", "poll", "punch"} {
		samples := stats.samples[kind]
		slices.Sort(samples)
		fmt.Printf("%-9s n=%-6d errors=%-4d p50=%-8s p95=%-8s p99=%-8s max=%s\n", kind, len(samples), stats.errors[kind],
			percentile(samples, 0.50).Round(time.Microsecond*100), percentile(samples, 0.95).Round(time.Microsecond*100),
			percentile(samples, 0.99).Round(time.Microsecond*100), percentile(samples, 1).Round(time.Microsecond*100))
		if stats.errors[kind] > 0 {
			failed = true
		}
	}
	if failed {
		return 1
	}
	return 0
}

// registerDevices signs in as a gym owner and registers each serial, skipping ones that exist.
func registerDevices(server string, serials []string) error {
	email, password := os.Getenv("DEVSIM_EMAIL"), os.Getenv("DEVSIM_PASSWORD")
	if email == "" || password == "" {
		return fmt.Errorf("set DEVSIM_EMAIL and DEVSIM_PASSWORD to a gym owner's login")
	}

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 10 * time.Second}

	post := func(path string, body any) (int, error) {
		raw, _ := json.Marshal(body)
		resp, err := client.Post(server+path, "application/json", bytes.NewReader(raw))
		if err != nil {
			return 0, err
		}
		resp.Body.Close()
		return resp.StatusCode, nil
	}

	code, err := post("/v1/auth/login", map[string]string{"email": email, "password": password})
	if err != nil {
		return err
	}
	if code != http.StatusOK {
		return fmt.Errorf("login: HTTP %d", code)
	}

	for i, serial := range serials {
		code, err := post("/v1/devices", map[string]string{"serialNumber": serial, "name": fmt.Sprintf("Load device %d", i+1)})
		if err != nil {
			return err
		}
		if code != http.StatusCreated && code != http.StatusConflict {
			return fmt.Errorf("register %s: HTTP %d", serial, code)
		}
	}
	fmt.Printf("registered %d devices\n", len(serials))
	return nil
}
