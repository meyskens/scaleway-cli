// Copyright (C) 2015 Scaleway. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE.md file.

// scw helpers

// Package utils contains helpers
package utils

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	log "github.com/scaleway/scaleway-cli/vendor/github.com/Sirupsen/logrus"
)

// quoteShellArgs transforms an array of shell arguments ([]string) into a copy/paste-able string
func quoteShellArgs(args []string) string {
	output := ""
	for _, arg := range args {
		output += " "
		output += strconv.Quote(arg)
	}
	return output
}

// SSHExec executes a command over SSH and redirects file-descriptors
func SSHExec(publicIPAddress string, privateIPAddress string, command []string, checkConnection bool, gatewayIPAddress string) error {
	if publicIPAddress == "" && gatewayIPAddress == "" {
		return errors.New("server does not have public IP")
	}
	if privateIPAddress == "" && gatewayIPAddress != "" {
		return errors.New("server does not have private IP")
	}

	if checkConnection {
		useGateway := gatewayIPAddress != ""
		if useGateway && !IsTCPPortOpen(fmt.Sprintf("%s:22", gatewayIPAddress)) {
			return errors.New("gateway is not available, try again later")
		}
		if !useGateway && !IsTCPPortOpen(fmt.Sprintf("%s:22", publicIPAddress)) {
			return errors.New("server is not ready, try again later")
		}
	}

	execCmd := append(NewSSHExecCmd(publicIPAddress, privateIPAddress, true, nil, command, gatewayIPAddress))

	log.Debugf("Executing: ssh %s", quoteShellArgs(execCmd))

	spawn := exec.Command("ssh", execCmd...)
	spawn.Stdout = os.Stdout
	spawn.Stdin = os.Stdin
	spawn.Stderr = os.Stderr
	return spawn.Run()
}

// NewSSHExecCmd computes execve compatible arguments to run a command via ssh
func NewSSHExecCmd(publicIPAddress string, privateIPAddress string, allocateTTY bool, sshOptions []string, command []string, gatewayIPAddress string) []string {
	useGateway := gatewayIPAddress != ""
	execCmd := []string{}

	if os.Getenv("DEBUG") != "1" {
		execCmd = append(execCmd, "-q")
	}

	if os.Getenv("exec_secure") != "1" {
		execCmd = append(execCmd, "-o", "UserKnownHostsFile=/dev/null", "-o", "StrictHostKeyChecking=no")
	}

	if len(sshOptions) > 0 {
		execCmd = append(execCmd, strings.Join(sshOptions, " "))
	}

	execCmd = append(execCmd, "-l", "root")
	if useGateway {
		proxyCommand := NewSSHExecCmd(gatewayIPAddress, "", allocateTTY, []string{"-W", "%h:%p"}, nil, "")
		execCmd = append(execCmd, privateIPAddress, "-o", "ProxyCommand=ssh "+strings.Join(proxyCommand, " "))
	} else {
		execCmd = append(execCmd, publicIPAddress)
	}

	if allocateTTY {
		execCmd = append(execCmd, "-t", "-t")
	}

	if len(command) > 0 {
		execCmd = append(execCmd, "--", "/bin/sh", "-e")

		if os.Getenv("DEBUG") == "1" {
			execCmd = append(execCmd, "-x")
		}

		execCmd = append(execCmd, "-c")

		execCmd = append(execCmd, fmt.Sprintf("%q", strings.Join(command, " ")))
	}

	return execCmd
}

// WaitForTCPPortOpen calls IsTCPPortOpen in a loop
func WaitForTCPPortOpen(dest string) error {
	for {
		if IsTCPPortOpen(dest) {
			break
		}
		time.Sleep(1 * time.Second)
	}
	return nil
}

// IsTCPPortOpen returns true if a TCP communication with "host:port" can be initialized
func IsTCPPortOpen(dest string) bool {
	conn, err := net.DialTimeout("tcp", dest, time.Duration(2000)*time.Millisecond)
	if err == nil {
		defer conn.Close()
	}
	return err == nil
}

// TruncIf ensures the input string does not exceed max size if cond is met
func TruncIf(str string, max int, cond bool) string {
	if cond && len(str) > max {
		return str[:max]
	}
	return str
}

// Wordify convert complex name to a single word without special shell characters
func Wordify(str string) string {
	str = regexp.MustCompile(`[^a-zA-Z0-9-]`).ReplaceAllString(str, "_")
	str = regexp.MustCompile(`__+`).ReplaceAllString(str, "_")
	str = strings.Trim(str, "_")
	return str
}

// PathToTARPathparts returns the two parts of a unix path
func PathToTARPathparts(fullPath string) (string, string) {
	fullPath = strings.TrimRight(fullPath, "/")
	return path.Dir(fullPath), path.Base(fullPath)
}

// RemoveDuplicates transforms an array into a unique array
func RemoveDuplicates(elements []string) []string {
	encountered := map[string]bool{}

	// Create a map of all unique elements.
	for v := range elements {
		encountered[elements[v]] = true
	}

	// Place all keys from the map into a slice.
	result := []string{}
	for key := range encountered {
		result = append(result, key)
	}
	return result
}

// GetHomeDir returns the path to your home
func GetHomeDir() (string, error) {
	homeDir := os.Getenv("HOME") // *nix
	if homeDir == "" {           // Windows
		homeDir = os.Getenv("USERPROFILE")
	}
	if homeDir == "" {
		return "", errors.New("user home directory not found")
	}
	return homeDir, nil
}

// GetConfigFilePath returns the path to the Scaleway CLI config file
func GetConfigFilePath() (string, error) {
	path, err := GetHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(path, ".scwrc"), nil
}

const termjsBin string = "termjs-cli"

// AttachToSerial tries to connect to server serial using 'term.js-cli' and fallback with a help message
func AttachToSerial(serverID string, apiToken string, attachStdin bool) error {
	termjsURL := fmt.Sprintf("https://tty.cloud.online.net?server_id=%s&type=serial&auth_token=%s", serverID, apiToken)

	args := []string{}
	if !attachStdin {
		args = append(args, "--no-stdin")
	}
	args = append(args, termjsURL)
	log.Debugf("Executing: %s %v", termjsBin, args)
	// FIXME: check if termjs-cli is installed
	spawn := exec.Command(termjsBin, args...)
	spawn.Stdout = os.Stdout
	spawn.Stdin = os.Stdin
	spawn.Stderr = os.Stderr
	err := spawn.Run()
	if err != nil {
		log.Warnf(`
You need to install '%s' from https://github.com/moul/term.js-cli

    npm install -g term.js-cli

However, you can access your serial using a web browser:

    %s

`, termjsBin, termjsURL)
		return err
	}
	return nil
}