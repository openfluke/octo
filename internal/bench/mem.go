package bench

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// MemSnap is a host memory sample around a generate run.
type MemSnap struct {
	RSSMB      float64 `json:"rss_mb"`
	HeapAllocMB float64 `json:"heap_alloc_mb"`
	HeapSysMB  float64 `json:"heap_sys_mb"`
	NumGC      uint32  `json:"num_gc"`
}

func sampleMem() MemSnap {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	s := MemSnap{
		HeapAllocMB: float64(ms.HeapAlloc) / (1024 * 1024),
		HeapSysMB:   float64(ms.HeapSys) / (1024 * 1024),
		NumGC:       ms.NumGC,
		RSSMB:       float64(ms.Sys) / (1024 * 1024), // fallback
	}
	if rss := linuxRSSMB(); rss > 0 {
		s.RSSMB = rss
	}
	return s
}

func linuxRSSMB() float64 {
	f, err := os.Open("/proc/self/status")
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "VmRSS:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0
		}
		kb, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			return 0
		}
		return kb / 1024
	}
	return 0
}
