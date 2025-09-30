package collector

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"math"
	"strings"
	"time"

	mb "github.com/goburrow/modbus"
)

// PointValue represents a decoded reading from a point.
// Value holds the scaled/offset value as float64 for uniformity.
// For boolean points, Value will be 0 or 1.
type PointValue struct {
	ServerID  string
	DeviceID  string
	Connection string
	SlaveID   uint8
	PointName string
	Address   uint16
	Register  string // holding|input|coil|discrete
	DataType  string
	ByteOrder string
	Unit      string
	Raw       any
	Value     float64
	Timestamp time.Time
}

// ResultHandler is a callback to process collected values.
// Return an error to have it logged by the collector.
type ResultHandler func(PointValue) error

// Collector manages polling a single device.
type Collector struct {
	Server  ServerConfig
	Device  Device
	Handler ResultHandler

	// generic handler for TCP or RTU
	handler  handlerWithConn
	connAddr string
}

// handlerWithConn embeds mb.ClientHandler and exposes Connect/Close used for lifecycle.
type handlerWithConn interface {
	mb.ClientHandler
	Connect() error
	Close() error
}

// newHandler creates and configures a handler for TCP or RTU based on config.
// It returns the handler and a human-readable address for logs.
func (c *Collector) newHandler() (handlerWithConn, string, error) {
	proto := strings.ToLower(strings.TrimSpace(c.Server.Protocol))
	timeout := c.Server.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	switch proto {
	case "modbus-tcp", "tcp":
		address := fmt.Sprintf("%s:%d", c.Server.Connection.Host, c.Server.Connection.Port)
		h := mb.NewTCPClientHandler(address)
		h.Timeout = timeout
		h.SlaveId = c.Device.SlaveID
		return h, address, nil
	case "modbus-rtu", "rtu":
		port := c.Server.Connection.SerialPort
		if strings.TrimSpace(port) == "" {
			return nil, "", fmt.Errorf("serial_port is required for RTU")
		}
		h := mb.NewRTUClientHandler(port)
		if c.Server.Connection.BaudRate > 0 {
			h.BaudRate = c.Server.Connection.BaudRate
		}
		if c.Server.Connection.DataBits > 0 {
			h.DataBits = c.Server.Connection.DataBits
		}
		if c.Server.Connection.StopBits > 0 {
			h.StopBits = c.Server.Connection.StopBits
		}
		if p := strings.ToUpper(strings.TrimSpace(c.Server.Connection.Parity)); p != "" {
			h.Parity = p
		}
		h.Timeout = timeout
		h.SlaveId = c.Device.SlaveID
		return h, port, nil
	default:
		return nil, "", fmt.Errorf("protocol %s not implemented", c.Server.Protocol)
	}
}

func (c *Collector) Run(ctx context.Context) error {
	// Build handler based on protocol
	h, addr, err := c.newHandler()
	if err != nil {
		return err
	}
	c.handler = h
	c.connAddr = addr

	// initial connect with simple retries
	retry := c.Server.RetryCount
	if retry < 0 {
		retry = 0
	}
	for attempts := 0; attempts <= retry; attempts++ {
		if err := h.Connect(); err != nil {
			if attempts == retry {
				return fmt.Errorf("connect %s: %w", addr, err)
			}
			select {
			case <-time.After(time.Second):
			case <-ctx.Done():
				return ctx.Err()
			}
			continue
		}
		break
	}
	defer h.Close()

	client := mb.NewClient(h)

	interval := c.Device.PollInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Immediate first run
	if err := c.pollOnce(ctx, client); err != nil {
		log.Printf("collector %s/%s initial poll: %v", c.Server.ServerID, c.Device.DeviceID, err)
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := c.pollOnce(ctx, client); err != nil {
				log.Printf("collector %s/%s poll: %v", c.Server.ServerID, c.Device.DeviceID, err)
			}
		}
	}
}

func (c *Collector) pollOnce(ctx context.Context, client mb.Client) error {
	for _, p := range c.Device.Points {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		val, err := c.readPoint(client, p)
		if err != nil {
			// Attempt one reconnect and retry
			if recErr := c.reconnect(); recErr == nil {
				if val2, err2 := c.readPoint(client, p); err2 == nil {
					val = val2
				} else {
					return fmt.Errorf("read point %s@%d: %w", p.Name, p.Address, err2)
				}
			} else {
				return fmt.Errorf("read point %s@%d: %w", p.Name, p.Address, err)
			}
		}
		if c.Handler != nil {
			if err := c.Handler(val); err != nil {
				log.Printf("handler error for %s/%s/%s: %v", c.Server.ServerID, c.Device.DeviceID, p.Name, err)
			}
		}
	}
	return nil
}

func (c *Collector) readPoint(client mb.Client, p Point) (PointValue, error) {
	rt := strings.ToLower(p.RegisterType)
	dt := strings.ToLower(p.DataType)
	bo := strings.ToUpper(p.ByteOrder)

	pv := PointValue{
		ServerID:  c.Server.ServerID,
		DeviceID:  c.Device.DeviceID,
		Connection: c.connAddr,
		SlaveID:  c.Device.SlaveID,
		PointName: p.Name,
		Address:   p.Address,
		Register:  rt,
		DataType:  dt,
		ByteOrder: bo,
		Unit:      p.Unit,
		Timestamp: time.Now(),
	}

	switch rt {
	case "holding":
		qty := uint16(1)
		if dt == "float32" || dt == "uint32" || dt == "int32" {
			qty = 2
		}
		data, err := client.ReadHoldingRegisters(p.Address, qty)
		if err != nil {
			return pv, err
		}
		return decodeRegisterData(pv, data, dt, bo, p)
	case "input":
		qty := uint16(1)
		if dt == "float32" || dt == "uint32" || dt == "int32" {
			qty = 2
		}
		data, err := client.ReadInputRegisters(p.Address, qty)
		if err != nil {
			return pv, err
		}
		return decodeRegisterData(pv, data, dt, bo, p)
	case "coil":
		data, err := client.ReadCoils(p.Address, 1)
		if err != nil {
			return pv, err
		}
		b := len(data) > 0 && (data[0]&0x01 == 0x01)
		pv.Raw = b
		pv.Value = boolToFloat(b)
		if pv.DataType == "" {
			pv.DataType = "bool"
		}
		return pv, nil
	case "discrete":
		data, err := client.ReadDiscreteInputs(p.Address, 1)
		if err != nil {
			return pv, err
		}
		b := len(data) > 0 && (data[0]&0x01 == 0x01)
		pv.Raw = b
		pv.Value = boolToFloat(b)
		if pv.DataType == "" {
			pv.DataType = "bool"
		}
		return pv, nil
	default:
		return pv, fmt.Errorf("unsupported register type: %s", p.RegisterType)
	}
}

func decodeRegisterData(pv PointValue, data []byte, dt, bo string, p Point) (PointValue, error) {
	applyScale := func(v float64) float64 { return v*p.Scale + p.Offset }
	if pv.DataType == "" {
		pv.DataType = dt
	}
	pv.ByteOrder = bo

	switch dt {
	case "uint16":
		if len(data) < 2 {
			return pv, errors.New("insufficient data for uint16")
		}
		u := binary.BigEndian.Uint16(data[:2])
		pv.Raw = u
		pv.Value = applyScale(float64(u))
		return pv, nil
	case "int16":
		if len(data) < 2 {
			return pv, errors.New("insufficient data for int16")
		}
		u := binary.BigEndian.Uint16(data[:2])
		i := int16(u)
		pv.Raw = i
		pv.Value = applyScale(float64(i))
		return pv, nil
	case "float32":
		if len(data) < 4 {
			return pv, errors.New("insufficient data for float32")
		}
		b := reorder32(data[:4], bo)
		u := binary.BigEndian.Uint32(b)
		f := math.Float32frombits(u)
		pv.Raw = f
		pv.Value = applyScale(float64(f))
		return pv, nil
	case "uint32":
		if len(data) < 4 {
			return pv, errors.New("insufficient data for uint32")
		}
		b := reorder32(data[:4], bo)
		u := binary.BigEndian.Uint32(b)
		pv.Raw = u
		pv.Value = applyScale(float64(u))
		return pv, nil
	case "int32":
		if len(data) < 4 {
			return pv, errors.New("insufficient data for int32")
		}
		b := reorder32(data[:4], bo)
		u := binary.BigEndian.Uint32(b)
		i := int32(u)
		pv.Raw = i
		pv.Value = applyScale(float64(i))
		return pv, nil
	default:
		return pv, fmt.Errorf("unsupported data type: %s", dt)
	}
}

// reorder32 returns a 4-byte slice reordered per byte-order string.
// Supported orders: "ABCD" (default), "DCBA", "BADC" (byte swap within words), "CDAB" (word swap).
func reorder32(in []byte, order string) []byte {
	var out [4]byte
	if len(in) < 4 {
		return append([]byte{}, in...)
	}
	switch strings.ToUpper(strings.TrimSpace(order)) {
	case "", "ABCD":
		copy(out[:], in[:4])
	case "DCBA":
		out[0], out[1], out[2], out[3] = in[3], in[2], in[1], in[0]
	case "BADC":
		out[0], out[1], out[2], out[3] = in[1], in[0], in[3], in[2]
	case "CDAB":
		out[0], out[1], out[2], out[3] = in[2], in[3], in[0], in[1]
	default:
		copy(out[:], in[:4])
	}
	return out[:]
}

func boolToFloat(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// reconnect attempts to close and reopen the underlying handler.
func (c *Collector) reconnect() error {
	if c.handler == nil {
		return errors.New("no handler")
	}
	c.handler.Close()
	time.Sleep(200 * time.Millisecond)
	return c.handler.Connect()
}
