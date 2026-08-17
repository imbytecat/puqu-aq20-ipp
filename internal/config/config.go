// Package config defines process bootstrap settings that must be known before serving requests.
package config

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
)

type Config struct {
	IPPListen   string `json:"ippListen"`
	AdminListen string `json:"adminListen"`
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.IPPListen) == "" || strings.TrimSpace(c.AdminListen) == "" {
		return errors.New("listen addresses are required")
	}
	if err := validateListenAddress(c.IPPListen, false); err != nil {
		return fmt.Errorf("invalid IPP listen address: %w", err)
	}
	if err := validateListenAddress(c.AdminListen, true); err != nil {
		return fmt.Errorf("invalid admin listen address: %w", err)
	}
	return nil
}

func validateListenAddress(address string, loopbackOnly bool) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return errors.New("port must be between 1 and 65535")
	}
	if !loopbackOnly {
		return nil
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return errors.New("admin listener must use localhost or a loopback IP")
	}
	return nil
}
