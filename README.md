# Relay

###### An extremely dangerous tool for executing arbitrary shell commands in the GUI login context on macOS

> [!WARNING]
> The risk associated with such a service should be obvious. DO NOT USE THIS TOOL.

---

## Overview

Relay opens a TCP listener for each specified interactive shell at a configurable address and port.
When a client connects to one of these listeners, it must first send an "end-of-file" (EOF) marker followed by a single newline. It then sends the contents of a shell script to execute, followed by the EOF marker on its own line, again followed by a newline.

Relay copies the script content received to disk until it reaches the EOF marker, executes the script in a new shell (corresponding to the listener the client connected to), and copies all output (stdout and stderr) back to the client and then closes the connection.

### Purpose

On macOS, not all shells have equivalent privileges. In particular, when a user logs in via SSH, the user does not have the same privileges as those given to the same user physically logged into the macOS desktop. When [relay is configured as a LaunchAgent](#configuration) in the GUI domain, a user connecting via SSH can "relay" commands through the LaunchAgent to effectively execute commands with GUI privileges.

## Usage

```
Usage: relay [options] shell[:addr][:port] ...

Arguments:
  shell[:addr][:port]  Shell name/path, optional listen address, optional port
                       Omitted addr/port use defaults from -l flag
                       Unspecified ports auto-increment for each service
                       Examples:
                         bash              (use default addr:port)
                         bash:192.168.0.1  (use default port)
                         bash::8080        (use default addr)
                         bash:192.168.0.1:8080

Options:
  -e IDENT=VALUE
        export IDENT=VALUE to shells
  -j    use JSON structured logging
  -l [ADDR]:PORT
        default listen [ADDR]:PORT for shells (addr defaults to 127.0.0.1) (default "127.0.0.1:50135")
  -o FILE
        output FILE for exported data (default: "-" for stdout) (default "-")
```

### Example

Either prepare the service as a LaunchAgent [as described in Configuration](#configuration), or run the server temporarily (from a GUI terminal logged in to the macOS desktop):

```sh
# Start a zsh service on default address:port (127.0.0.1:50135)
relay zsh

# Start multiple shells with auto-incrementing ports
relay zsh bash sh  # zsh on :50135, bash on :50136, sh on :50137

# Specify custom addresses and ports
relay bash:192.168.1.100:8080 zsh::9000  # bash on 192.168.1.100:8080, zsh on 127.0.0.1:9000

# Set a different default listen address/port
relay -l 0.0.0.0:9000 bash sh  # bash on 0.0.0.0:9000, sh on 0.0.0.0:9001

# Export environment variables to shells
relay -e FOO=bar -e BAZ=qux bash  # bash will have FOO and BAZ set
```

To send commands to execute, you can use a tool such as netcat (`nc`) or `telnet`, for example:

```sh
# Send a command to the shell service
echo -e "EOF\nls -la\nEOF\n" | nc localhost 50135

# Send a shell script to execute
nc localhost 50135 < <( echo _EOF_; cat script.sh; echo _EOF_; )
```

A [relay-command.sh](docs/relay-command.sh) shell script is also provided that allows you to simply provide the commands to execute as command-line arguments, which is convenient for single commands:

```sh
relay-command.sh ls -la
```

## Installation

There are two options to install the `relay` executable without manually retrieving and compiling from source:

### Build with standard toolchain (_recommended_)

```sh
go install -v github.com/ardnew/relay@latest
```

### Download binary release package

Visit the [releases page](https://github.com/ardnew/relay/releases) to download the latest binary release for your platform.

## Configuration

To install the `relay` service as a LaunchAgent, copy the provided [LaunchAgent plist file](docs/com.github.ardnew.relay.plist) to your service directory, update any paths defined in it, and load it with:

```sh
# Note you _must_ use the full absolute file path with launchctl.
# Using "~" or relative paths will result in an error.
launchctl load -w /Users/you/Library/LaunchAgents/com.github.ardnew.relay.plist
launchctl enable gui/$( id -u )/com.github.ardnew.relay
launchctl start gui/$( id -u )/com.github.ardnew.relay
```
