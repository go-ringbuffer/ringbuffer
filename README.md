# Golang ring buffer module

## `import "gopkg.in/ringbuffer.v0"`

![Go Import](https://img.shields.io/badge/import-gopkg.in/ringbuffer.v0-9cf?logo=go&style=for-the-badge)
[![Go Reference](https://img.shields.io/badge/reference-go.dev-007d9c?logo=go&style=for-the-badge)](https://pkg.go.dev/gopkg.in/ringbuffer.v0)

Adapted from https://github.com/smallnest/ringbuffer removing mutex. Added `io.ReaderFrom` and `io.WriterTo` interfaces.
Added optimized bytes operators.
