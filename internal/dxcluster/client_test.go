package dxcluster

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

func TestParseSpotKeepsExactFrequencyAndUTCDate(t *testing.T) {
	now := time.Date(2026, 11, 24, 0, 2, 0, 0, time.UTC)
	spot, ok := parseSpot("DX de EA4ABC:  14195.567  EA8/EA4ABC  UP 5    2359Z", now)
	if !ok || spot.Hz != 14195567 || spot.Reporter != "EA4ABC" || spot.Comment != "UP 5" || !spot.At.Equal(now.Add(-3*time.Minute)) {
		t.Fatalf("unexpected spot: %+v (valid = %v)", spot, ok)
	}
	if spot.Entity.Flag != "ic" || spot.Entity.Zone != Africa {
		t.Fatalf("portable DXCC: %+v", spot.Entity)
	}
	if _, ok := parseSpot("DX de EA4ABC: 14195.5 EA4ABC 2500Z", now); ok {
		t.Fatal("accepted invalid UTC time")
	}
}

func TestLookupUsesDXCCExceptionsAndZones(t *testing.T) {
	for _, want := range []struct {
		call, flag string
		zone       Zone
	}{
		{"F4ABC", "fr", Europe},
		{"KL7ABC", "us", NorthAmerica},
		{"VP2EABC", "ai", CentralAmerica},
		{"PY2ABC", "br", SouthAmerica},
		{"JA1ABC", "jp", Asia},
		{"VK2ABC", "au", Oceania},
		{"EA4ABC/EA8/P", "ic", Africa},
		{"4U1UN", "un", NorthAmerica},
		{"EA4ABC/MM", "", All},
	} {
		if got := Lookup(want.call); got.Flag != want.flag || got.Zone != want.zone {
			t.Errorf("%s resolved to %+v, want %s / %v", want.call, got, want.flag, want.zone)
		}
	}
	flags := Flags()
	for _, code := range []string{"fr", "jp", "au", "br", "un"} {
		if len(flags[code]) == 0 {
			t.Errorf("missing image for %s", code)
		}
	}
}

func TestReceiveLogsInAndReadsLiveSpot(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	served := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			served <- err
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(2 * time.Second))
		reader := bufio.NewReader(conn)
		for _, step := range []struct{ prompt, want string }{{"login: ", "EA4ABC"}, {"Password: ", "secret"}} {
			if _, err := fmt.Fprint(conn, step.prompt); err != nil {
				served <- err
				return
			}
			line, err := reader.ReadString('\n')
			if err != nil || strings.TrimSpace(line) != step.want {
				served <- fmt.Errorf("unexpected login exchange: %v", err)
				return
			}
		}
		_, err = fmt.Fprintf(conn, "\r\nDX de EA3ABC: 14195.5 F4ABC CQ %sZ\r\n", time.Now().UTC().Format("1504"))
		served <- err
	}()
	var got Spot
	receive(ctx, listener.Addr().String(), "EA4ABC", "secret", func(event Event) {
		if event.Spot.Hz > 0 {
			got = event.Spot
		}
	})
	if err := <-served; err != nil {
		t.Fatal(err)
	}
	if got.Call != "F4ABC" || got.Hz != 14195500 {
		t.Fatalf("unexpected received spot: %+v", got)
	}
}

func TestParseRecentDXFunWidget(t *testing.T) {
	now := time.Date(2026, 11, 24, 0, 2, 0, 0, time.UTC)
	body := `<table><tr><td>
<table><TR>
<td>&nbsp;De&nbsp;<a href=https://www.qrz.com/db/VA2RB TARGET=_BLANK>VA2RB</span></TD>
<td>7001.123</TD><td><IMG SRC="../images/banderes/237.gif"></TD>
<td><a href=https://www.qrz.com/db/OX7AM><b>OX7AM</b></a></TD>
<td colspan=8><i>IOTA &amp; CQ</i></TD><td>23:59&nbsp;</TD>
</tr></tr></table></td></tr></table>`
	spots, err := parseRecent(body, now)
	if err != nil || len(spots) != 1 {
		t.Fatalf("parseRecent: %+v, %v", spots, err)
	}
	got := spots[0]
	if got.Call != "OX7AM" || got.Reporter != "VA2RB" || got.Hz != 7001123 || got.Comment != "IOTA & CQ" || !got.At.Equal(now.Add(-3*time.Minute)) {
		t.Fatalf("unexpected web spot: %+v", got)
	}
}
