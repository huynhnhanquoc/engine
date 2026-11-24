package runtime

// ipTable gives each address of a program's code the index of the decoded instruction that starts there,
// -1 for an address inside an instruction: a slice over the code's bytes where the verifier kept a
// map[int]int, which was most of what verifying a program allocated and much of its time (survey,
// 03/11/2026: the verifier was 16-27% of a compile).
type ipTable []int32

func newIPTable(codeLength int) ipTable {
	table := make(ipTable, codeLength)
	for address := range table {
		table[address] = -1
	}
	return table
}

// at is the index of the instruction that starts at ip; false for any other address.
func (table ipTable) at(ip int) (int, bool) {
	if ip < 0 || ip >= len(table) || table[ip] < 0 {
		return 0, false
	}
	return int(table[ip]), true
}

// boundary reports an address an instruction starts at, or the end of the code: where a jump may land.
func (table ipTable) boundary(ip int) bool {
	return ip == len(table) || (ip >= 0 && ip < len(table) && table[ip] >= 0)
}

// stackPlaces is what analyzeStack proved of each instruction (its stack depth and its function), by the
// instruction's index among the decoded ones; entry is -1 for an instruction no entry point reaches.
type stackPlaces struct {
	byIP   ipTable
	states []stackState
}

// at is what was proved of the instruction at ip; false when no entry point reaches it.
func (places stackPlaces) at(ip int) (stackState, bool) {
	index, ok := places.byIP.at(ip)
	if !ok || places.states[index].entry < 0 {
		return stackState{}, false
	}
	return places.states[index], true
}
