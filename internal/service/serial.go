package service

import (
	"fmt"
	"strconv"
	"strings"
)

// CleanSerial accepts an ADB network address such as 192.168.1.8:5555.
// An empty value means the caller is creating a container, not attaching a device.
func CleanSerial(raw string) (string, int, error) {
	serial := strings.TrimSpace(raw)
	serial = strings.TrimPrefix(serial, "adb connect ")
	serial = strings.TrimSpace(serial)
	if serial == "" {
		return "", 0, nil
	}
	if strings.ContainsAny(serial, " \t\r\n/\\") {
		return "", 0, fmt.Errorf("%w: adb address must be host:port", ErrValidation)
	}
	host, portText, ok := strings.Cut(serial, ":")
	if !ok || host == "" || strings.Contains(portText, ":") {
		return "", 0, fmt.Errorf("%w: adb address must be host:port", ErrValidation)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return "", 0, fmt.Errorf("%w: adb port is invalid", ErrValidation)
	}
	return host + ":" + portText, port, nil
}
