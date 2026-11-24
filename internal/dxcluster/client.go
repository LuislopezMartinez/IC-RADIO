package dxcluster

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const address = "dxfun.com:8000"

type Spot struct {
	Call, Reporter, Comment string
	Hz                      int64
	At                      time.Time
	Entity                  Entity
}

type Event struct {
	Spot   Spot
	Status string
}

var spotLine = regexp.MustCompile(`(?i)^DX de\s+([A-Z0-9/\-]+):\s+(\d{1,7}(?:\.\d{1,3})?)\s+([A-Z0-9/]{3,25})\s+(.*?)\s*(\d{4})Z(?:\s.*)?$`)
var loginCall = regexp.MustCompile(`^[A-Z0-9]{3,12}(?:-[0-9]{1,2})?$`)

func ValidLogin(call string) bool {
	return loginCall.MatchString(call) && strings.ContainsAny(call, "0123456789") && strings.ContainsAny(call, "ABCDEFGHIJKLMNOPQRSTUVWXYZ")
}

func parseSpot(line string, now time.Time) (Spot, bool) {
	fields := spotLine.FindStringSubmatch(strings.TrimSpace(line))
	if len(fields) == 0 {
		return Spot{}, false
	}
	whole, decimal, _ := strings.Cut(fields[2], ".")
	khz, _ := strconv.ParseInt(whole, 10, 64)
	hz, _ := strconv.ParseInt((decimal + "000")[:3], 10, 64)
	hour, _ := strconv.Atoi(fields[5][:2])
	minute, _ := strconv.Atoi(fields[5][2:])
	if khz == 0 || hour >= 24 || minute >= 60 {
		return Spot{}, false
	}
	now = now.UTC()
	at := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, time.UTC)
	if at.After(now) {
		at = at.AddDate(0, 0, -1)
	}
	call := strings.ToUpper(fields[3])
	return Spot{Call: call, Reporter: fields[1], Comment: strings.TrimSpace(fields[4]), Hz: khz*1000 + hz, At: at, Entity: Lookup(call)}, true
}

func Run(ctx context.Context, call, password string, events chan<- Event) {
	defer close(events)
	send := func(event Event) {
		select {
		case events <- event:
		case <-ctx.Done():
		}
	}
	for ctx.Err() == nil {
		send(Event{Status: "Conectando a DXFun…"})
		err := receive(ctx, address, call, password, send)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			send(Event{Status: err.Error()})
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(15 * time.Second):
		}
	}
}

func receive(ctx context.Context, address, call, password string, send func(Event)) error {
	dialer := net.Dialer{Timeout: 8 * time.Second, KeepAlive: 30 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("DXFun sin conexión. Reintentando…")
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	conn.SetDeadline(time.Now().Add(12 * time.Second))
	reader := bufio.NewReader(conn)
	if _, err := reader.ReadString(':'); err != nil {
		return fmt.Errorf("DXFun no respondió al iniciar sesión")
	}
	if _, err := fmt.Fprintf(conn, "%s\r\n", call); err != nil {
		return fmt.Errorf("DXFun cerró la conexión")
	}
	// The node's password prompt may arrive without a newline.
	var line []byte
	passwordSent := false
	seenSpot := false
	send(Event{Status: "DXFun · esperando spots"})
	for ctx.Err() == nil {
		conn.SetReadDeadline(time.Now().Add(3 * time.Minute))
		b, err := reader.ReadByte()
		if err != nil {
			return fmt.Errorf("DXFun desconectado. Reintentando…")
		}
		if b == 255 {
			command, err := reader.ReadByte()
			if err != nil {
				return err
			}
			if command >= 251 && command <= 254 {
				option, err := reader.ReadByte()
				if err != nil {
					return err
				}
				if command == 251 || command == 253 {
					response := byte(254)
					if command == 253 {
						response = 252
					}
					conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
					if _, err = conn.Write([]byte{255, response, option}); err != nil {
						return err
					}
				}
			}
			continue
		}
		if b == '\n' {
			if spot, ok := parseSpot(string(line), time.Now()); ok {
				if !seenSpot {
					send(Event{Status: "DXFun conectado"})
					seenSpot = true
				}
				send(Event{Spot: spot})
			}
			line = line[:0]
			continue
		}
		if len(line) >= 4096 {
			continue
		}
		line = append(line, b)
		if b == ':' {
			prompt := strings.ToLower(strings.TrimSpace(string(line)))
			if prompt == "password:" || prompt == "contraseña:" {
				if passwordSent || password == "" {
					return fmt.Errorf("DXFun solicita una contraseña. Revisa el acceso")
				}
				conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				if _, err := fmt.Fprintf(conn, "%s\r\n", password); err != nil {
					return err
				}
				passwordSent = true
				line = line[:0]
			}
		}
	}
	return nil
}
