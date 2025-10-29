# Makefile for Modbus Simulator
# Targets mirror commands documented in README.md

SHELL := bash

# Config paths
CONFIG_TOML ?= config.topway.toml
CONFIG_YAML ?= config/topway_config.yaml
MOCKTTY_YAML ?= config/mocktty.yaml

# Defaults for batch runs
N ?= 3                   # number of clients for `make clients`
SNAP_JSON ?= out.json    # for `make servers-snapshot`
SNAP_CSV  ?= out.csv     # for `make servers-snapshot`
SNAP_WAIT ?= 5s          # for `make servers-snapshot`

.PHONY: help server client clients servers servers-snapshot mocktty

help:
	@echo "Modbus Simulator - Make targets"
	@echo ""
	@echo "Single instance:"
	@echo "  make server                # run single Modbus server (cmd/server)"
	@echo "  make client                # run single client against server (cmd/client)"
	@echo ""
	@echo "Multiple clients:"
	@echo "  make clients N=5           # run N clients concurrently (default N=$(N))"
	@echo ""
	@echo "Concurrent servers manager:"
	@echo "  make servers               # run servers from YAML (cmd/servers)"
	@echo "  make servers-snapshot      # run, wait, export snapshot JSON/CSV then exit"
	@echo "       (params: SNAP_JSON=$(SNAP_JSON) SNAP_CSV=$(SNAP_CSV) SNAP_WAIT=$(SNAP_WAIT))"
	@echo ""
	@echo "Mock serial (RTU-over-TCP):"
	@echo "  make mocktty               # run RTU-over-TCP mock endpoints (cmd/mocktty)"
	@echo ""
	@echo "Variables: CONFIG_TOML=$(CONFIG_TOML) CONFIG_YAML=$(CONFIG_YAML)"

# --- Single server/client ---
server:
	go run ./cmd/server --config $(CONFIG_TOML)

client:
	go run ./cmd/client --config $(CONFIG_TOML)

# Run N clients concurrently and wait for all to finish
collectors:
	go run ./cmd/collector --config $(CONFIG_YAML)

# --- Concurrent servers manager ---
servers:
	go run ./cmd/servers --config $(CONFIG_YAML)

# Start servers, wait for SNAP_WAIT, export snapshot, exit
servers-snapshot:
	go run ./cmd/servers --config $(CONFIG_YAML) \
		--snapshot-json $(SNAP_JSON) \
		--snapshot-csv  $(SNAP_CSV) \
		--snapshot-wait $(SNAP_WAIT)

# --- RTU-over-TCP mock serial ---
mocktty:
	go run ./cmd/mocktty --config $(MOCKTTY_YAML)
