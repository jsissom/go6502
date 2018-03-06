package asm

import (
	"bytes"
	"testing"
)

func assemble(t *testing.T, code string) []byte {
	r := bytes.NewReader([]byte(code))
	result, err := Assemble(r, false)
	if err != nil {
		t.Error(err)
		return []byte{}
	}
	return result.Code
}

func fromHex(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	default:
		return 0
	}
}

func checkASM(t *testing.T, asm string, expected string) {
	code := assemble(t, asm)

	b := make([]byte, len(code)*2)
	for i, j := 0, 0; i < len(code); i, j = i+1, j+2 {
		v := code[i]
		b[j+0] = hex[v>>4]
		b[j+1] = hex[v&0x0f]
	}
	s := string(b)

	if s != expected {
		t.Error("code doesn't match expected")
		t.Errorf("got: %s\n", s)
		t.Errorf("exp: %s\n", expected)
	}
}

func TestAddressingIMM(t *testing.T) {
	asm := `
	LDA #$20
	LDX #$20
	LDY #$20
	ADC #$20
	SBC #$20
	CMP #$20
	CPX #$20
	CPY #$20
	AND #$20
	ORA #$20
	EOR #$20`

	checkASM(t, asm, "A920A220A0206920E920C920E020C020292009204920")
}

func TestAddressingABS(t *testing.T) {
	asm := `
	LDA $2000
	LDX $2000
	LDY $2000
	STA $2000
	STX $2000
	STY $2000
	ADC $2000
	SBC $2000
	CMP $2000
	CPX $2000
	CPY $2000
	BIT $2000
	AND $2000
	ORA $2000
	EOR $2000
	INC $2000
	DEC $2000
	JMP $2000
	JSR $2000
	ASL $2000
	LSR $2000
	ROL $2000
	ROR $2000
	LDA A:$20
	LDA ABS:$20`

	checkASM(t, asm, "AD0020AE0020AC00208D00208E00208C00206D0020ED0020CD0020"+
		"EC0020CC00202C00202D00200D00204D0020EE0020CE00204C00202000200E0020"+
		"4E00202E00206E0020AD2000AD2000")
}

func TestAddressingABX(t *testing.T) {
	asm := `
	LDA $2000,X
	LDY $2000,X
	STA $2000,X
	ADC $2000,X
	SBC $2000,X
	CMP $2000,X
	AND $2000,X
	ORA $2000,X
	EOR $2000,X
	INC $2000,X
	DEC $2000,X
	ASL $2000,X
	LSR $2000,X
	ROL $2000,X
	ROR $2000,X`

	checkASM(t, asm, "BD0020BC00209D00207D0020FD0020DD00203D00201D00205D0020"+
		"FE0020DE00201E00205E00203E00207E0020")
}

func TestAddressingABY(t *testing.T) {
	asm := `
	LDA $2000,Y
	LDX $2000,Y
	STA $2000,Y
	ADC $2000,Y
	SBC $2000,Y
	CMP $2000,Y
	AND $2000,Y
	ORA $2000,Y
	EOR $2000,Y`

	checkASM(t, asm, "B90020BE0020990020790020F90020D90020390020190020590020")
}

func TestAddressingZPG(t *testing.T) {
	asm := `
	LDA $20
	LDX $20
	LDY $20
	STA $20
	STX $20
	STY $20
	ADC $20
	SBC $20
	CMP $20
	CPX $20
	CPY $20
	BIT $20
	AND $20
	ORA $20
	EOR $20
	INC $20
	DEC $20
	ASL $20
	LSR $20
	ROL $20
	ROR $20`

	checkASM(t, asm, "A520A620A4208520862084206520E520C520E420C42024202520"+
		"05204520E620C6200620462026206620")
}

func TestAddressingIND(t *testing.T) {
	asm := `
	JMP ($20)
	JMP ($2000)`

	checkASM(t, asm, "6C20006C0020")
}

func TestDataBytes(t *testing.T) {
	asm := `
	.DB "AB", $00
	.DB 'f, 'f'
	.DB $ABCD
	.DB $ABCD >> 8
	.DB $0102
	.DB $03040506
	.DB 1+2+3+4
	.DB -1
	.DB -129
	.DB 0b0101010101010101
	.DB 0b01010101`

	checkASM(t, asm, "4142006666CDAB02060AFF7F5555")
}

func TestDataWords(t *testing.T) {
	asm := `
	.DW "AB", $00
	.DW 'f, 'f'
	.DW $ABCD
	.DW $ABCD >> 8
	.DW $0102
	.DW $03040506
	.DW 1+2+3+4
	.DW -1
	.DW -129
	.DW 0b01010101
	.DW 0b0101010101010101`

	checkASM(t, asm, "4142000066006600CDABAB00020106050A00FFFF7FFF55005555")
}

func TestDataDwords(t *testing.T) {
	asm := `
	.DD "AB", $00
	.DD 'f, 'f'
	.DD $ABCD
	.DD $ABCD >> 8
	.DD $0102
	.DD $03040506
	.DD 1+2+3+4
	.DD -1
	.DD -129
	.DD 0b01010101
	.DD 0b0101010101010101`

	checkASM(t, asm, "4142000000006600000066000000CDAB0000AB000000020100000"+
		"60504030A000000FFFFFFFF7FFFFFFF5500000055550000")
}

func TestDataHexStrings(t *testing.T) {
	asm := `
	.DH 0102030405060708
	.DH aabbcc
	.DH dd
	.DH ee
	.DH ff`

	checkASM(t, asm, "0102030405060708AABBCCDDEEFF")
}

func TestDataTermStrings(t *testing.T) {
	asm := `
	.DS "AAA"
	.DS "a", 0
	.DS ""`

	checkASM(t, asm, "4141C1E100")
}

func TestAlign(t *testing.T) {
	asm := `
	.ALIGN 4
	.DB $ff
	.ALIGN 2
	.DB $ff
	.ALIGN 8
	.DB $ff
	.ALIGN 1
	.DB $ff`

	checkASM(t, asm, "FF00FF0000000000FFFF")
}
