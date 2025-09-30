package modbusdb

import (
	"context"

	dbpkg "modbus-simulator/internal/db"
	"modbus-simulator/internal/model"
)

// Client exposes a stable API for third-party packages to access the DB.
// Placed in servers.go so that all other files can reference it.
type Client struct{ db *dbpkg.DB }

// Open opens the SQLite database (runs migrations) and returns a client.
func Open(path string) (*Client, error) {
	d, err := dbpkg.Open(path)
	if err != nil {
		return nil, err
	}
	return &Client{db: d}, nil
}

// Close closes the underlying DB.
func (c *Client) Close() error { return c.db.Close() }

// --------------------
// Server DTOs and converters
// --------------------

type Server struct {
	ServerID     string
	ServerName   string
	Protocol     string
	Host         string
	Port         int
	Timeout      string
	RetryCount   int
	Enabled      bool
	PollInterval string
}

func toModelServer(s *Server) *model.Server {
	if s == nil {
		return nil
	}
	return &model.Server{
		ServerID:     s.ServerID,
		ServerName:   s.ServerName,
		Protocol:     s.Protocol,
		Host:         s.Host,
		Port:         s.Port,
		Timeout:      s.Timeout,
		RetryCount:   s.RetryCount,
		Enabled:      s.Enabled,
		PollInterval: s.PollInterval,
	}
}

func fromModelServer(s *model.Server) *Server {
	if s == nil {
		return nil
	}
	return &Server{
		ServerID:     s.ServerID,
		ServerName:   s.ServerName,
		Protocol:     s.Protocol,
		Host:         s.Host,
		Port:         s.Port,
		Timeout:      s.Timeout,
		RetryCount:   s.RetryCount,
		Enabled:      s.Enabled,
		PollInterval: s.PollInterval,
	}
}

// --------------------
// Server management (CRUD)
// --------------------

func (c *Client) CreateServer(ctx context.Context, s *Server) error {
	return dbpkg.CreateServer(ctx, c.db.ORM, toModelServer(s))
}

func (c *Client) GetServer(ctx context.Context, serverID string) (*Server, error) {
	s, err := dbpkg.GetServer(ctx, c.db.ORM, serverID)
	if err != nil {
		return nil, err
	}
	return fromModelServer(s), nil
}

func (c *Client) ListServers(ctx context.Context) ([]Server, error) {
	list, err := dbpkg.ListServers(ctx, c.db.ORM)
	if err != nil {
		return nil, err
	}
	out := make([]Server, 0, len(list))
	for i := range list {
		out = append(out, *fromModelServer(&list[i]))
	}
	return out, nil
}

func (c *Client) UpdateServer(ctx context.Context, s *Server) error {
	return dbpkg.UpdateServer(ctx, c.db.ORM, toModelServer(s))
}

func (c *Client) DeleteServer(ctx context.Context, serverID string) error {
	return dbpkg.DeleteServer(ctx, c.db.ORM, serverID)
}

// SaveServer is a convenience upsert-like method (delegates to UpdateServer).
func (c *Client) SaveServer(ctx context.Context, s *Server) error {
	return dbpkg.UpdateServer(ctx, c.db.ORM, toModelServer(s))
}
