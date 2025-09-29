package modbus

// Thread-safe read helpers for building snapshots

import "fmt"

// GetHoldingRegister returns the current holding register value at address.
func GetHoldingRegister(s *Server, address uint16) (uint16, error) {
	// access protected by read lock
	s.mu.RLock()
	defer s.mu.RUnlock()
	if int(address) >= len(s.HoldingRegisters) {
		return 0, ErrAddrOutOfRange(address)
	}
	return s.HoldingRegisters[address], nil
}

// GetInputRegister returns the current input register value at address.
func GetInputRegister(s *Server, address uint16) (uint16, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if int(address) >= len(s.InputRegisters) {
		return 0, ErrAddrOutOfRange(address)
	}
	return s.InputRegisters[address], nil
}

// GetCoil returns the current coil value at address.
func GetCoil(s *Server, address uint16) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if int(address) >= len(s.Coils) {
		return false, ErrAddrOutOfRange(address)
	}
	return s.Coils[address], nil
}

// GetDiscreteInput returns the current discrete input value at address.
func GetDiscreteInput(s *Server, address uint16) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if int(address) >= len(s.DiscreteInputs) {
		return false, ErrAddrOutOfRange(address)
	}
	return s.DiscreteInputs[address], nil
}

// ErrAddrOutOfRange returns a formatted error compatible with server.go style.
func ErrAddrOutOfRange(addr uint16) error {
	return fmt.Errorf("address %d out of range", addr)
}
