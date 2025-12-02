package logging

import (
	"fmt"
	"net"
	"time"

	"github.com/rs/zerolog"
)

type Context struct {
	zl zerolog.Context
}

func NewContext(logger *zerolog.Logger) *Context {
	return &Context{zl: logger.With()}
}

func (c *Context) Logger() *zerolog.Logger {
	logger := c.zl.Logger()
	return &logger
}

func (context *Context) With() *Context {
	return &Context{zl: context.zl.Logger().With()}
}

func (context *Context) Log() *zerolog.Event {
	return context.Logger().Log()
}

func (context *Context) Trace() *zerolog.Event {
	return context.Logger().Trace()
}

func (context *Context) Debug() *zerolog.Event {
	return context.Logger().Debug()
}

func (context *Context) Info() *zerolog.Event {
	return context.Logger().Info()
}

func (context *Context) Warn() *zerolog.Event {
	return context.Logger().Warn()
}

func (context *Context) Error() *zerolog.Event {
	return context.Logger().Error()
}

func (context *Context) Fatal() *zerolog.Event {
	// IMPORTANT: Use WithLevel(zerolog.FatalLevel) here instead of Fatal()
	// to avoid the logger calling os.Exit().
	return context.Logger().WithLevel(zerolog.FatalLevel)
}

func (context *Context) Panic() *zerolog.Event {
	// IMPORTANT: Use WithLevel(zerolog.PanicLevel) here instead of Panic()
	// to avoid the logger calling panic()
	return context.Logger().WithLevel(zerolog.FatalLevel)
}

func (c *Context) IsDebugLevel() bool {
	return c.zl.Logger().GetLevel() <= zerolog.DebugLevel
}

func (c *Context) IsTraceLevel() bool {
	return c.zl.Logger().GetLevel() <= zerolog.TraceLevel
}

func (c *Context) AnErr(key string, err error) *Context {
	return &Context{zl: c.zl.AnErr(key, err)}
}

func (c *Context) Any(key string, i interface{}) *Context {
	return &Context{zl: c.zl.Any(key, i)}
}

func (c *Context) Array(key string, arr zerolog.LogArrayMarshaler) *Context {
	return &Context{zl: c.zl.Array(key, arr)}
}

func (c *Context) Bool(key string, val bool) *Context {
	return &Context{zl: c.zl.Bool(key, val)}
}
func (c *Context) Bools(key string, b []bool) *Context {
	return &Context{zl: c.zl.Bools(key, b)}
}

func (c *Context) Byte(key string, val byte) *Context {
	return &Context{zl: c.zl.Bytes(key, []byte{val})}
}
func (c *Context) Bytes(key string, val []byte) *Context {
	return &Context{zl: c.zl.Bytes(key, val)}
}

func (c *Context) Caller() *Context {
	c.zl.CallerWithSkipFrameCount(1)
	return c
}

func (c *Context) CallerWithSkipFrameCount(skipFrameCount int) *Context {
	c.zl.CallerWithSkipFrameCount(skipFrameCount + 1)
	return c
}

func (c *Context) Dict(key string, dict *zerolog.Event) *Context {
	return &Context{zl: c.zl.Dict(key, dict)}
}

func (c *Context) Dur(key string, d time.Duration) *Context {
	return &Context{zl: c.zl.Dur(key, d)}
}
func (c *Context) Durs(key string, d []time.Duration) *Context {
	return &Context{zl: c.zl.Durs(key, d)}
}

func (c *Context) EmbedObject(o zerolog.LogObjectMarshaler) *Context {
	return &Context{zl: c.zl.EmbedObject(o)}
}

func (c *Context) Err(err error) *Context {
	return &Context{zl: c.zl.Err(err)}
}
func (c *Context) Errs(key string, err []error) *Context {
	return &Context{zl: c.zl.Errs(key, err)}
}

func (c *Context) Fields(fields []interface{}) *Context {
	return &Context{zl: c.zl.Fields(fields)}
}

func (c *Context) Float32(key string, f float32) *Context {
	return &Context{zl: c.zl.Float32(key, f)}
}
func (c *Context) Floats32(key string, f []float32) *Context {
	return &Context{zl: c.zl.Floats32(key, f)}
}

func (c *Context) Float64(key string, f float64) *Context {
	return &Context{zl: c.zl.Float64(key, f)}
}
func (c *Context) Floats64(key string, f []float64) *Context {
	return &Context{zl: c.zl.Floats64(key, f)}
}

func (c *Context) Hex(key string, val []byte) *Context {
	return &Context{zl: c.zl.Hex(key, val)}
}

func (c *Context) Int(key string, val int) *Context {
	return &Context{zl: c.zl.Int(key, val)}
}
func (c *Context) Ints(key string, i []int) *Context {
	return &Context{zl: c.zl.Ints(key, i)}
}

func (c *Context) Int8(key string, i int8) *Context {
	return &Context{zl: c.zl.Int8(key, i)}
}
func (c *Context) Ints8(key string, i []int8) *Context {
	return &Context{zl: c.zl.Ints8(key, i)}
}

func (c *Context) Int16(key string, i int16) *Context {
	return &Context{zl: c.zl.Int16(key, i)}
}
func (c *Context) Ints16(key string, i []int16) *Context {
	return &Context{zl: c.zl.Ints16(key, i)}
}

func (c *Context) Int32(key string, i int32) *Context {
	return &Context{zl: c.zl.Int32(key, i)}
}
func (c *Context) Ints32(key string, i []int32) *Context {
	return &Context{zl: c.zl.Ints32(key, i)}
}

func (c *Context) Int64(key string, i int64) *Context {
	return &Context{zl: c.zl.Int64(key, i)}
}
func (c *Context) Ints64(key string, i []int64) *Context {
	return &Context{zl: c.zl.Ints64(key, i)}
}

func (c *Context) Interface(key string, val any) *Context {
	return &Context{zl: c.zl.Interface(key, val)}
}

func (c *Context) IPAddr(key string, ip net.IP) *Context {
	return &Context{zl: c.zl.IPAddr(key, ip)}
}

func (c *Context) IPPrefix(key string, pfx net.IPNet) *Context {
	return &Context{zl: c.zl.IPPrefix(key, pfx)}
}

func (c *Context) MACAddr(key string, ha net.HardwareAddr) *Context {
	return &Context{zl: c.zl.MACAddr(key, ha)}
}

func (c *Context) Object(key string, obj zerolog.LogObjectMarshaler) *Context {
	return &Context{zl: c.zl.Object(key, obj)}
}

func (c *Context) RawJSON(key string, b []byte) *Context {
	return &Context{zl: c.zl.RawJSON(key, b)}
}

func (c *Context) Reset() *Context {
	return &Context{zl: c.zl.Reset()}
}

func (c *Context) Stack() *Context {
	return &Context{zl: c.zl.Stack()}
}

func (c *Context) Str(key string, val string) *Context {
	return &Context{zl: c.zl.Str(key, val)}
}

func (c *Context) Stringer(key string, val fmt.Stringer) *Context {
	return &Context{zl: c.zl.Stringer(key, val)}
}

func (c *Context) Strs(key string, vals []string) *Context {
	return &Context{zl: c.zl.Strs(key, vals)}
}

func (c *Context) Time(key string, t time.Time) *Context {
	return &Context{zl: c.zl.Time(key, t)}
}
func (c *Context) Times(key string, t []time.Time) *Context {
	return &Context{zl: c.zl.Times(key, t)}
}

func (c *Context) Timestamp() *Context {
	return &Context{zl: c.zl.Timestamp()}
}

func (c *Context) Type(key string, val interface{}) *Context {
	return &Context{zl: c.zl.Type(key, val)}
}

func (c *Context) Uint(key string, i uint) *Context {
	return &Context{zl: c.zl.Uint(key, i)}
}
func (c *Context) Uints(key string, i []uint) *Context {
	return &Context{zl: c.zl.Uints(key, i)}
}

func (c *Context) Uint8(key string, i uint8) *Context {
	return &Context{zl: c.zl.Uint8(key, i)}
}
func (c *Context) Uints8(key string, i []uint8) *Context {
	return &Context{zl: c.zl.Uints8(key, i)}
}

func (c *Context) Uint16(key string, i uint16) *Context {
	return &Context{zl: c.zl.Uint16(key, i)}
}
func (c *Context) Uints16(key string, i []uint16) *Context {
	return &Context{zl: c.zl.Uints16(key, i)}
}

func (c *Context) Uint32(key string, i uint32) *Context {
	return &Context{zl: c.zl.Uint32(key, i)}
}
func (c *Context) Uints32(key string, i []uint32) *Context {
	return &Context{zl: c.zl.Uints32(key, i)}
}

func (c *Context) Uint64(key string, i uint64) *Context {
	return &Context{zl: c.zl.Uint64(key, i)}
}
func (c *Context) Uints64(key string, i []uint64) *Context {
	return &Context{zl: c.zl.Uints64(key, i)}
}
