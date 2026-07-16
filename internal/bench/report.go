package bench

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// writeTableReport writes a human-readable table next to the JSON log.
func writeTableReport(log ResultLog, jsonPath string) (string, error) {
	out := strings.TrimSuffix(jsonPath, filepath.Ext(jsonPath)) + ".txt"
	var b strings.Builder

	dur := log.FinishedAt.Sub(log.StartedAt).Round(time.Millisecond)
	fmt.Fprintf(&b, "Octo bench report\n")
	fmt.Fprintf(&b, "=================\n")
	fmt.Fprintf(&b, "template : %s\n", log.Template)
	fmt.Fprintf(&b, "repo     : %s\n", log.Repo)
	fmt.Fprintf(&b, "host     : %s\n", log.Host)
	fmt.Fprintf(&b, "platform : %s\n", log.Platform)
	fmt.Fprintf(&b, "started  : %s\n", log.StartedAt.Local().Format(time.RFC3339))
	fmt.Fprintf(&b, "finished : %s\n", log.FinishedAt.Local().Format(time.RFC3339))
	fmt.Fprintf(&b, "duration : %s\n", dur)
	fmt.Fprintf(&b, "summary  : total=%d ok=%d failed=%d skipped=%d\n\n",
		log.Summary.Total, log.Summary.OK, log.Summary.Failed, log.Summary.Skipped)

	// Column widths
	type row struct {
		status, quant, profile, msg string
		prefill, decode, total      string
		prefTok, genTok             string
		prefMS, decMS               string
		rss, heap, entity           string
		note                        string
	}
	rows := make([]row, 0, len(log.Runs))
	for _, r := range log.Runs {
		rr := row{
			quant:   r.Quantize,
			profile: r.Profile,
			msg:     fmt.Sprintf("%d", r.MessageIndex),
		}
		switch {
		case r.Skipped != "":
			rr.status = "skip"
			rr.note = truncate(r.Skipped, 48)
		case r.Error != "":
			rr.status = "ERR"
			rr.note = truncate(r.Error, 48)
		case r.Metrics != nil:
			rr.status = "ok"
			m := r.Metrics
			rr.prefill = fmt.Sprintf("%.1f", m.PrefillTokPerSec)
			rr.decode = fmt.Sprintf("%.1f", m.DecodeTokPerSec)
			rr.total = fmt.Sprintf("%.1f", m.TotalTokPerSec)
			rr.prefTok = fmt.Sprintf("%d", m.PrefillTokens)
			rr.genTok = fmt.Sprintf("%d", m.GeneratedTokens)
			rr.prefMS = fmt.Sprintf("%d", m.PrefillMS)
			rr.decMS = fmt.Sprintf("%d", m.DecodeMS)
			rr.rss = fmt.Sprintf("%.0f→%.0f", m.RSSMBBefore, m.RSSMBAfter)
			rr.heap = fmt.Sprintf("%.0f", m.HeapAllocMB)
			rr.entity = fmt.Sprintf("%.1f", m.EntityMB)
		default:
			rr.status = "?"
		}
		rows = append(rows, rr)
	}

	headers := []string{
		"status", "quant", "profile", "msg",
		"prefill", "decode", "total",
		"pTok", "gTok", "pMS", "dMS",
		"rssMB", "heapMB", "entMB", "note",
	}
	cols := make([][]string, len(headers))
	for i, h := range headers {
		cols[i] = []string{h}
	}
	for _, r := range rows {
		vals := []string{
			r.status, r.quant, r.profile, r.msg,
			r.prefill, r.decode, r.total,
			r.prefTok, r.genTok, r.prefMS, r.decMS,
			r.rss, r.heap, r.entity, r.note,
		}
		for i, v := range vals {
			if v == "" {
				v = "-"
			}
			cols[i] = append(cols[i], v)
		}
	}
	widths := make([]int, len(cols))
	for i, col := range cols {
		for _, cell := range col {
			if n := len(cell); n > widths[i] {
				widths[i] = n
			}
		}
	}

	writeSep := func() {
		b.WriteByte('+')
		for _, w := range widths {
			b.WriteString(strings.Repeat("-", w+2))
			b.WriteByte('+')
		}
		b.WriteByte('\n')
	}
	writeLine := func(cells []string) {
		b.WriteByte('|')
		for i, cell := range cells {
			fmt.Fprintf(&b, " %-*s |", widths[i], cell)
		}
		b.WriteByte('\n')
	}

	writeSep()
	writeLine(headers)
	writeSep()
	for i := range rows {
		cells := make([]string, len(headers))
		for c := range headers {
			cells[c] = cols[c][i+1]
		}
		writeLine(cells)
	}
	writeSep()

	// Compact reply appendix (ok runs only)
	hasReply := false
	for _, r := range log.Runs {
		if r.Reply != "" && r.Error == "" && r.Skipped == "" {
			hasReply = true
			break
		}
	}
	if hasReply {
		b.WriteString("\nReplies\n-------\n")
		for _, r := range log.Runs {
			if r.Reply == "" || r.Error != "" || r.Skipped != "" {
				continue
			}
			fmt.Fprintf(&b, "[%s / %s msg%d] %s\n", r.Quantize, r.Profile, r.MessageIndex, truncate(oneLine(r.Reply), 120))
		}
	}

	if err := os.WriteFile(out, []byte(b.String()), 0o644); err != nil {
		return "", err
	}
	return out, nil
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if n <= 0 || len(s) <= n {
		return s
	}
	if n < 4 {
		return s[:n]
	}
	return s[:n-3] + "..."
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return strings.Join(strings.Fields(s), " ")
}
