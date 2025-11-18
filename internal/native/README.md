# jetkvm-native

This component (`internal/native/`) acts as a bridge between Golang and native (C/C++) code.
It manages spawning and communicating with a native process via sockets (gRPC and Unix stream).

For performance-critical operations such as video frame, **a dedicated Unix socket should be used** to avoid the overhead of gRPC and ensure low-latency communication.

## Debugging

To enable debug mode, create a file called `.native-debug-mode` in the `/userdata/jetkvm` directory.

```bash
touch /userdata/jetkvm/.native-debug-mode
```

This will cause the native process to listen for SIGHUP signal and crash the process.

```bash
pgrep native | xargs kill -SIGHUP
```