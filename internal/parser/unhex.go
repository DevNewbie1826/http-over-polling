package parser

var unhex = initUnhex()

func initUnhex() [256]int8 {
	table := [256]int8{}
	for i := range table {
		table[i] = -1
	}
	for ch := byte('0'); ch <= '9'; ch++ {
		table[ch] = int8(ch - '0')
	}
	for ch := byte('a'); ch <= 'f'; ch++ {
		table[ch] = int8(ch-'a') + 10
	}
	for ch := byte('A'); ch <= 'F'; ch++ {
		table[ch] = int8(ch-'A') + 10
	}
	return table
}
