package main

import (
    "bytes"
    "fmt"
    "os"
)

type ReadEEPROMBytewise struct {
    Filename string `arg help:"New output file, never overwritten."`
    Count int `optional default:"2048" help:"Byte count (1..2048)."`
}

func (r ReadEEPROMBytewise) Run(c *Context) error {
    if !CLI.NoPatch || CLI.NoFirmware || CLI.EEPROMSize != 2048 {
        return fmt.Errorf("requires --no-patch --eeprom-size=2048 and no --no-firmware")
    }
    if r.Count < 1 || r.Count > 2048 { return fmt.Errorf("invalid count") }
    f, err := os.OpenFile(r.Filename, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0444)
    if err != nil { return err }
    defer f.Close()
    for addr := 0; addr < r.Count; addr++ {
        request := []byte{0xe5, byte(addr >> 8), byte(addr), 0, 0, 0, 0, 0}
        reply, err := c.hal.ROMExchangeReport(request)
        if err != nil { return fmt.Errorf("EEPROM address %04x: %w (partial file retained)", addr, err) }
        if len(reply) != 8 || !bytes.Equal(reply[:3], request[:3]) {
            return fmt.Errorf("invalid EEPROM response at %04x: %x (partial file retained)", addr, reply)
        }
        if _, err := f.Write(reply[3:4]); err != nil { return err }
    }
    if err := f.Sync(); err != nil { return err }
    fmt.Printf("Read %d EEPROM bytes with one-byte ROM requests; no model-specific bulk mode used.\n", r.Count)
    return nil
}
