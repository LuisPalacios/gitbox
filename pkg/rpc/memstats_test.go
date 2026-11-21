package rpc

import "runtime"

// testingMemStats reads the cumulative bytes allocated by the process.
type testingMemStats struct{ total uint64 }

func (m *testingMemStats) read() {
	var s runtime.MemStats
	runtime.ReadMemStats(&s)
	m.total = s.TotalAlloc
}
