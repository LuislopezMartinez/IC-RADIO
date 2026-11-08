package ptt

import (
	"fmt"
	"sync"

	"go.bug.st/serial"
	"go.bug.st/serial/enumerator"
)

type Line string

const (
	RTS Line = "RTS"
	DTR Line = "DTR"
)

type Port struct {
	Name, Description string
}

func Ports() []Port {
	details, err := enumerator.GetDetailedPortsList()
	if err != nil {
		return nil
	}
	result := make([]Port, 0, len(details))
	for _, item := range details {
		description := item.Product
		if description == "" {
			description = item.Name
		}
		result = append(result, Port{Name: item.Name, Description: description})
	}
	return result
}

// Controller drives a transceiver's USB PTT/SEND line. It always clears both modem
// control lines when opening and closing the port so the radio returns to RX.
type Controller struct {
	mu       sync.Mutex
	port     serial.Port
	portName string
	line     Line
	tx       bool
}

func (c *Controller) Connect(portName string, line Line) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closeLocked()
	if portName == "" {
		return fmt.Errorf("select a COM port")
	}
	port, err := serial.Open(portName, &serial.Mode{
		BaudRate: 115200,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
		// The library otherwise opens with both lines asserted. Setting the
		// initial bits prevents even a momentary, unintended transmission.
		InitialStatusBits: &serial.ModemOutputBits{DTR: false, RTS: false},
	})
	if err != nil {
		return fmt.Errorf("open %s: %w", portName, err)
	}
	if err := port.SetRTS(false); err != nil {
		_ = port.Close()
		return fmt.Errorf("disable RTS: %w", err)
	}
	if err := port.SetDTR(false); err != nil {
		_ = port.Close()
		return fmt.Errorf("disable DTR: %w", err)
	}
	c.port, c.portName, c.line = port, portName, line
	return nil
}

func (c *Controller) SetTX(enabled bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.port == nil {
		return fmt.Errorf("PTT not connected")
	}
	var err error
	if c.line == DTR {
		err = c.port.SetDTR(enabled)
	} else {
		err = c.port.SetRTS(enabled)
	}
	if err != nil {
		c.tx = false
		return fmt.Errorf("control %s: %w", c.line, err)
	}
	c.tx = enabled
	return nil
}

func (c *Controller) Connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.port != nil
}

func (c *Controller) TX() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tx
}

func (c *Controller) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closeLocked()
}

func (c *Controller) closeLocked() {
	if c.port != nil {
		_ = c.port.SetRTS(false)
		_ = c.port.SetDTR(false)
		_ = c.port.Close()
	}
	c.port, c.portName, c.tx = nil, "", false
}
