package mshal

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

type cbApplyParamType func(h *HAL, out []byte) error
type cbPostExchangeType func(h *HAL, addr int, buf []byte) error

var diagnosticWriteRanges [][2]int

// Only a reviewed diagnostic operation may temporarily enable RAM writes.
func DiagnosticWriteRanges(ranges [][2]int) { diagnosticWriteRanges = ranges }

func diagnosticAllowed(out [9]byte) bool {
	if out[1] == 0xb5 || out[1] == 0xe5 || out[1] == 0xa5 { return true }
	if out[1] != 0xb6 { return false }
	addr := int(out[2])<<8 | int(out[3])
	for _, r := range diagnosticWriteRanges { if addr >= r[0] && addr < r[1] { return true } }
	return false
}

type romCommand struct {
	id byte

	is16bit    bool
	isWrite    bool
	maxPayload int
	offset     int
	addrShift  int

	cbApplyParam   cbApplyParamType
	cbPostExchange cbPostExchangeType
}

func romCommandMake(id byte, is16bit bool, isWrite bool) romCommand {
	return romCommand{
		id:      id,
		is16bit: is16bit,
		isWrite: isWrite,
	}
}

func romCommandMakeReadWrite(id byte, is16bit bool) (romCommand, romCommand) {
	return romCommandMake(id, is16bit, false), romCommandMake(id+1, is16bit, true)
}

func (h *HAL) romExchangeReport(out [9]byte, checkLen int) ([9]byte, error) {
	var in [9]byte
	// Diagnostic build: only original ROM read commands are allowed.
	if !diagnosticAllowed(out) {
		return in, fmt.Errorf("diagnostic read-only guard rejected opcode 0x%02x", out[1])
	}

	if h.config.LogFunc != nil {
		h.config.LogFunc(3, "ROMOut:   %s", hex.EncodeToString(out[:]))
	}

	if n, err := h.dev.SendFeatureReport(out[:]); err != nil {
		return in, err
	} else if n != len(out) {
		return in, fmt.Errorf("short HID send: %d", n)
	}

	n, err := h.dev.GetFeatureReport(in[:])
	if err != nil {
		return in, err
	}
	if n != len(in) {
		return in, fmt.Errorf("short HID response: %d", n)
	}

	if h.config.LogFunc != nil {
		h.config.LogFunc(3, "ROMIn:    %s", hex.EncodeToString(in[:]))
	}

	if !bytes.Equal(out[:checkLen], in[:checkLen]) {
		return in, ErrorInvalidResponse
	}
	return in, nil
}

func (h *HAL) ROMExchangeReport(out []byte) ([]byte, error) {
	var outb [9]byte
	if len(out) > 8 {
		return nil, errors.New("buffer longer than 8 bytes")
	}
	copy(outb[1:], out)
	inb, err := h.romExchangeReport(outb, 0)
	return inb[1:], err
}

func (h *HAL) romProtocolMakeHeader(cmd byte, is16bit bool, addr int, index int) ([9]byte, int) {
	var out [9]byte

	out[0] = 0
	out[1] = byte(cmd)

	if index == 0 {
		index = 2
	}
	if is16bit {
		out[index] = byte(addr >> 8)
		index++
	}
	out[index] = byte(addr)
	index++

	return out, index
}

func (h *HAL) romProtocolReadReply(buf []byte, in []byte, index int, maxReply int) int {
	if len(buf) > maxReply {
		buf = buf[:maxReply]
	}
	return copy(buf, in[index:])

}

func (h *HAL) romProtocolWritePayload(buf []byte, out []byte, index int, maxLen int) int {
	if len(buf) > maxLen {
		buf = buf[:maxLen]
	}
	return copy(out[index:], buf)
}

func (h *HAL) romProtocolExec(cmd romCommand, addr int, buf []byte) (int, error) {
	if len(buf) == 0 {
		return 0, nil
	}

	maxPayload := cmd.maxPayload
	if maxPayload <= 0 {
		maxPayload = 1
	}

	addr >>= cmd.addrShift

	out, index := h.romProtocolMakeHeader(cmd.id, cmd.is16bit, addr, cmd.offset)

	txrLen := 0
	if cmd.isWrite {
		txrLen = h.romProtocolWritePayload(buf, out[:], index, maxPayload)
	}

	if cmd.cbApplyParam != nil {
		if err := cmd.cbApplyParam(h, out[:]); err != nil {
			return 0, err
		}
	}

	in, err := h.romExchangeReport(out, index)
	if err != nil {
		return 0, err
	}

	if !cmd.isWrite {
		txrLen = h.romProtocolReadReply(buf, in[:], index, maxPayload)
	}

	if cmd.cbPostExchange != nil {
		if err := cmd.cbPostExchange(h, addr, buf); err != nil {
			return 0, err
		}
	}

	return txrLen, nil
}

type halROMMemoryRegion struct {
	hal          *HAL
	readCommand  *romCommand
	writeCommand *romCommand
	baseAddr     int
	length       int
	name         MemoryRegionNameType
	alignment    int
}

func (h halROMMemoryRegion) GetName() MemoryRegionNameType {
	return h.name
}

func (h halROMMemoryRegion) GetLength() int {
	return h.length
}

func (h halROMMemoryRegion) GetParent() (MemoryRegion, int) {
	return nil, 0
}

func (h halROMMemoryRegion) GetAlignment() int {
	return h.alignment
}

func (h halROMMemoryRegion) Access(write bool, addr int, buf []byte) (int, error) {
	if addr > h.length {
		return 0, nil
	}
	if addr+len(buf) > h.length {
		buf = buf[:h.length-addr]
	}

	if write {
		if h.writeCommand == nil {
			return 0, ErrorWriteNotAllowed
		}
		return h.hal.romProtocolExec(*h.writeCommand, h.baseAddr+addr, buf)
	}

	if h.readCommand == nil {
		return 0, ErrorReadNotAllowed
	}
	return h.hal.romProtocolExec(*h.readCommand, h.baseAddr+addr, buf)
}

func (h *HAL) romMemoryRegionMake(name MemoryRegionNameType, baseAddr int, length int, alignment int, read *romCommand, write *romCommand) MemoryRegion {
	return regionWrapCompleteIO(halROMMemoryRegion{
		hal:          h,
		baseAddr:     baseAddr,
		length:       length,
		readCommand:  read,
		writeCommand: write,
		name:         name,
		alignment:    alignment,
	})
}

func romEepromV2HandleTwoByteAddress(h *HAL, out []byte) error {
	if h.eepromSize > 2048 {
		out[8] = 1
	}
	return nil
}

func romEepromVerify(region MemoryRegion) cbPostExchangeType {
	return func(h *HAL, addr int, buf []byte) error {
		if buf[0] == 0 {
			/* The chip returns 0 if there is no I2C response, so we just have to wait */
			time.Sleep(15 * time.Millisecond)
			return nil
		}

		var tmp [1]byte
		for i := 0; i < 25; i++ {
			if _, err := region.Access(false, addr, tmp[:]); err != nil {
				return err
			}
			if tmp[0] == buf[0] {
				return nil
			}
		}
		return ErrorTimeout
	}
}
