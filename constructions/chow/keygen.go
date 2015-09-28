package chow

import (
	"../../primitives/encoding"
	"../../primitives/matrix"
	"../../primitives/table"
	"../saes"
	"crypto/aes"
	"crypto/cipher"
	"io"
)

type Side int

const (
	Left = iota
	Right
)

type DevNull struct{}

func (dn DevNull) Read(p []byte) (n int, err error) {
	for i := 0; i < len(p); i++ {
		p[i] = 0
	}

	return len(p), nil
}

func generateStream(seed, label [16]byte) io.Reader {
	// Generate sub-key
	subKey := [16]byte{}
	c, _ := aes.NewCipher(seed[:])
	c.Encrypt(subKey[:], label[:])

	// Create pseudo-random byte stream keyed by sub-key.
	block, _ := aes.NewCipher(subKey[:])
	stream := cipher.StreamReader{
		cipher.NewCTR(block, label[:]),
		DevNull{},
	}

	return stream
}

// Encodes the output of a T-Box/Tyi Table / the input of a top-level XOR.
//
//    position: Position in the state array, counted in *bytes*.
// subPosition: Position in the T-Box/Tyi Table's ouptput for this byte, counted in nibbles.
func TyiEncoding(seed [16]byte, round, position, subPosition int) encoding.Nibble {
	label := [16]byte{}
	label[0], label[1], label[2], label[3] = 'T', byte(round), byte(position), byte(subPosition)

	return encoding.GenerateShuffle(generateStream(seed, label))
}

func MBInverseEncoding(seed [16]byte, round, position, subPosition int) encoding.Nibble {
	label := [16]byte{}
	label[0], label[1], label[2], label[3], label[4] = 'M', 'E', byte(round), byte(position), byte(subPosition)

	return encoding.GenerateShuffle(generateStream(seed, label))
}

// Encodes intermediate results between the two top-level XORs and the bottom XOR.
// The bottom XOR decodes its input with a Left and Right XOREncoding and encodes its output with a RoundEncoding.
//
// position: Position in the state array, counted in nibbles.
//     side: "Side" of the circuit. Left for the (a ^ b) side and Right for the (c ^ d) side.
func HighXOREncoding(seed [16]byte, round, position int, side Side) encoding.Nibble {
	label := [16]byte{}
	label[0], label[1], label[2], label[3], label[4] = 'H', 'X', byte(round), byte(position), byte(side)

	return encoding.GenerateShuffle(generateStream(seed, label))
}

func LowXOREncoding(seed [16]byte, round, position int, side Side) encoding.Nibble {
	label := [16]byte{}
	label[0], label[1], label[2], label[3], label[4] = 'L', 'X', byte(round), byte(position), byte(side)

	return encoding.GenerateShuffle(generateStream(seed, label))
}

// Encodes the output of each round / the input of the next round's T-Box/Tyi Table.
//
// position: Position in the state array, counted in nibbles.
func RoundEncoding(seed [16]byte, round, position int) encoding.Nibble {
	label := [16]byte{}
	label[0], label[1], label[2], label[3] = 'R', 'O', byte(round), byte(position)

	return encoding.GenerateShuffle(generateStream(seed, label))
}

func InterRoundEncoding(seed [16]byte, round, position int) encoding.Nibble {
	label := [16]byte{}
	label[0], label[1], label[2], label[3] = 'R', 'I', byte(round), byte(position)

	return encoding.GenerateShuffle(generateStream(seed, label))
}

func ByteMixingBijection(seed [16]byte, round, position int) matrix.ByteMatrix {
	label := [16]byte{}
	label[0], label[1], label[2], label[3] = 'M', 'B', byte(round), byte(position)

	return matrix.GenerateRandomByte(generateStream(seed, label))
}

func WordMixingBijection(seed [16]byte, round, column int) matrix.WordMatrix {
	label := [16]byte{}
	label[0], label[1], label[2], label[3] = 'M', 'W', byte(round), byte(column)

	return matrix.GenerateRandomWord(generateStream(seed, label))
}

// Index in, index out.  Example: shiftRows(5) = 1 because ShiftRows(block) returns [16]byte{block[0], block[5], ...
func shiftRows(i int) int {
	return []int{0, 13, 10, 7, 4, 1, 14, 11, 8, 5, 2, 15, 12, 9, 6, 3}[i]
}

func GenerateKeys(key [16]byte, seed [16]byte) (out Construction) {
	constr := saes.Construction{key}
	roundKeys := constr.StretchedKey()

	// Apply ShiftRows to round keys 0 to 9.
	for k := 0; k < 10; k++ {
		roundKeys[k] = constr.ShiftRows(roundKeys[k])
	}

	for round := 0; round < 9; round++ {
		for pos := 0; pos < 16; pos++ {
			mb := WordMixingBijection(seed, round, pos/4)
			mbInv, _ := mb.Invert()

			var inEnc encoding.Byte

			if round == 0 {
				inEnc = encoding.IdentityByte{}
			} else {
				inEnc = encoding.ComposedBytes{
					encoding.ByteLinear(ByteMixingBijection(seed, round-1, pos)),
					encoding.ConcatenatedByte{
						RoundEncoding(seed, round-1, 2*pos+0),
						RoundEncoding(seed, round-1, 2*pos+1),
					},
				}
			}

			// Build the T-Box and Tyi Table for this round and position in the state matrix.
			out.TBoxTyiTable[round][pos] = encoding.WordTable{
				inEnc,
				encoding.ComposedWords{
					encoding.ConcatenatedWord{
						encoding.ByteLinear(ByteMixingBijection(seed, round, shiftRows(pos/4*4+0))),
						encoding.ByteLinear(ByteMixingBijection(seed, round, shiftRows(pos/4*4+1))),
						encoding.ByteLinear(ByteMixingBijection(seed, round, shiftRows(pos/4*4+2))),
						encoding.ByteLinear(ByteMixingBijection(seed, round, shiftRows(pos/4*4+3))),
					},
					encoding.WordLinear(mb),
					encoding.ConcatenatedWord{
						encoding.ConcatenatedByte{TyiEncoding(seed, round, pos, 0), TyiEncoding(seed, round, pos, 1)},
						encoding.ConcatenatedByte{TyiEncoding(seed, round, pos, 2), TyiEncoding(seed, round, pos, 3)},
						encoding.ConcatenatedByte{TyiEncoding(seed, round, pos, 4), TyiEncoding(seed, round, pos, 5)},
						encoding.ConcatenatedByte{TyiEncoding(seed, round, pos, 6), TyiEncoding(seed, round, pos, 7)},
					},
				},
				table.ComposedToWord{
					TBox{constr, roundKeys[round][pos], 0},
					TyiTable(pos % 4),
				},
			}

			out.MBInverseTable[round][pos] = encoding.WordTable{
				encoding.ConcatenatedByte{
					InterRoundEncoding(seed, round, 2*pos+0),
					InterRoundEncoding(seed, round, 2*pos+1),
				},
				encoding.ConcatenatedWord{
					encoding.ConcatenatedByte{MBInverseEncoding(seed, round, pos, 0), MBInverseEncoding(seed, round, pos, 1)},
					encoding.ConcatenatedByte{MBInverseEncoding(seed, round, pos, 2), MBInverseEncoding(seed, round, pos, 3)},
					encoding.ConcatenatedByte{MBInverseEncoding(seed, round, pos, 4), MBInverseEncoding(seed, round, pos, 5)},
					encoding.ConcatenatedByte{MBInverseEncoding(seed, round, pos, 6), MBInverseEncoding(seed, round, pos, 7)},
				},
				MBInverseTable{mbInv, uint(pos) % 4},
			}
		}

		// Generate the High and Low XOR Tables
		for pos := 0; pos < 32; pos++ {
			out.HighXORTable[round][pos][0] = encoding.NibbleTable{
				encoding.ConcatenatedByte{
					TyiEncoding(seed, round, pos/8*4+0, pos%8),
					TyiEncoding(seed, round, pos/8*4+1, pos%8),
				},
				HighXOREncoding(seed, round, pos, Left),
				XORTable{},
			}

			out.HighXORTable[round][pos][1] = encoding.NibbleTable{
				encoding.ConcatenatedByte{
					TyiEncoding(seed, round, pos/8*4+2, pos%8),
					TyiEncoding(seed, round, pos/8*4+3, pos%8),
				},
				HighXOREncoding(seed, round, pos, Right),
				XORTable{},
			}

			out.HighXORTable[round][pos][2] = encoding.NibbleTable{
				encoding.ConcatenatedByte{
					HighXOREncoding(seed, round, pos, Left),
					HighXOREncoding(seed, round, pos, Right),
				},
				InterRoundEncoding(seed, round, pos),
				XORTable{},
			}

			out.LowXORTable[round][pos][0] = encoding.NibbleTable{
				encoding.ConcatenatedByte{
					MBInverseEncoding(seed, round, pos/8*4+0, pos%8),
					MBInverseEncoding(seed, round, pos/8*4+1, pos%8),
				},
				LowXOREncoding(seed, round, pos, Left),
				XORTable{},
			}

			out.LowXORTable[round][pos][1] = encoding.NibbleTable{
				encoding.ConcatenatedByte{
					MBInverseEncoding(seed, round, pos/8*4+2, pos%8),
					MBInverseEncoding(seed, round, pos/8*4+3, pos%8),
				},
				LowXOREncoding(seed, round, pos, Right),
				XORTable{},
			}

			out.LowXORTable[round][pos][2] = encoding.NibbleTable{
				encoding.ConcatenatedByte{
					LowXOREncoding(seed, round, pos, Left),
					LowXOREncoding(seed, round, pos, Right),
				},
				RoundEncoding(seed, round, 2*shiftRows(pos/2)+pos%2),
				XORTable{},
			}
		}
	}

	// 10th T-Box
	for pos := 0; pos < 16; pos++ {
		out.TBox[pos] = encoding.ByteTable{
			encoding.ComposedBytes{
				encoding.ByteLinear(ByteMixingBijection(seed, 8, pos)),
				encoding.ConcatenatedByte{RoundEncoding(seed, 8, 2*pos+0), RoundEncoding(seed, 8, 2*pos+1)},
			},
			encoding.IdentityByte{},
			TBox{constr, roundKeys[9][pos], roundKeys[10][pos]},
		}
	}

	return
}
