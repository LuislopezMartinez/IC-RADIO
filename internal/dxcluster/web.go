package dxcluster

import (
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const webEndpoint = "https://www.dxfuncluster.com/entuweb.php"

var webClient = &http.Client{Timeout: 12 * time.Second}
var webRow = regexp.MustCompile(`(?i)<tr\b[^>]*>`)
var webRowEnd = regexp.MustCompile(`(?i)</tr\s*>`)
var webCell = regexp.MustCompile(`(?is)<td\b[^>]*>(.*?)</td\s*>`)
var htmlTag = regexp.MustCompile(`<[^>]*>`)
var webCall = regexp.MustCompile(`^[A-Z0-9/\-]{3,25}$`)
var webTime = regexp.MustCompile(`^([0-9]{2}):([0-9]{2})$`)

func Recent(ctx context.Context, band string) ([]Spot, error) {
	url := fmt.Sprintf("%s?banda=%s&%d", webEndpoint, band, time.Now().UnixMilli())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	response, err := webClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("DXFun web: HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 512*1024))
	if err != nil {
		return nil, err
	}
	return parseRecent(string(body), time.Now())
}

func parseRecent(body string, now time.Time) ([]Spot, error) {
	text := func(cell string) string {
		return strings.TrimSpace(html.UnescapeString(htmlTag.ReplaceAllString(cell, "")))
	}
	var spots []Spot
	// The widget nests a single spot row in a table for each entry.
	for _, row := range webRow.Split(body, -1)[1:] {
		if end := webRowEnd.FindStringIndex(row); end != nil {
			row = row[:end[0]]
		}
		cells := webCell.FindAllStringSubmatch(row, -1)
		if len(cells) != 6 {
			continue
		}
		reporter := strings.ToUpper(strings.TrimPrefix(text(cells[0][1]), "De\u00a0"))
		call := strings.ToUpper(text(cells[3][1]))
		hhmm := webTime.FindStringSubmatch(text(cells[5][1]))
		frequency := text(cells[1][1])
		if !webCall.MatchString(reporter) || !webCall.MatchString(call) || len(hhmm) == 0 {
			continue
		}
		// Use the same decimal-kHz and UTC handling as the live stream.
		line := fmt.Sprintf("DX de %s: %s %s %s %s%sZ", reporter, frequency, call, text(cells[4][1]), hhmm[1], hhmm[2])
		if spot, ok := parseSpot(line, now); ok {
			spots = append(spots, spot)
		}
	}
	if len(spots) == 0 && !strings.Contains(strings.ToLower(body), "spots") {
		return nil, fmt.Errorf("DXFun web: respuesta inesperada (%d bytes)", len(body))
	}
	return spots, nil
}
