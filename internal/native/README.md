# jetkvm-native

This component (`internal/native/`) acts as a bridge between Golang and native (C/C++) code.
It manages spawning and communicating with a native process via sockets (gRPC and Unix stream).

For performance-critical operations such as video frame, **a dedicated Unix socket should be used** to avoid the overhead of gRPC and ensure low-latency communication.