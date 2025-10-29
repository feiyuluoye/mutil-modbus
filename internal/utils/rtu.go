package utils

import (
    "context"
    "io"
    "os/exec"
    "time"

    "github.com/goburrow/serial"
)

type SerialParams struct {
    Address  string
    BaudRate int
    DataBits int
    StopBits int
    Parity   string
    Timeout  time.Duration
}

func EnsureSerialDefaults(sp *SerialParams) {
    if sp.BaudRate == 0 { sp.BaudRate = 9600 }
    if sp.DataBits == 0 { sp.DataBits = 8 }
    if sp.StopBits == 0 { sp.StopBits = 1 }
    if sp.Parity == "" { sp.Parity = "N" }
    if sp.Timeout <= 0 { sp.Timeout = 10 * time.Second }
}

func OpenSerial(sp SerialParams) (io.ReadWriteCloser, error) {
    EnsureSerialDefaults(&sp)
    sc := &serial.Config{
        Address:  sp.Address,
        BaudRate: sp.BaudRate,
        DataBits: sp.DataBits,
        StopBits: sp.StopBits,
        Parity:   sp.Parity,
        Timeout:  sp.Timeout,
    }
    return serial.Open(sc)
}

type SocatPair struct {
    Link string
    Peer string
}

func BuildSocatPairCmd(ctx context.Context, pair SocatPair) *exec.Cmd {
    cmd := exec.CommandContext(ctx, "socat",
        "-d", "-d",
        "pty,raw,echo=0,link="+pair.Link,
        "pty,raw,echo=0,link="+pair.Peer,
    )
    return cmd
}
