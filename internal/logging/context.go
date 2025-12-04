package logging

import (
	"fmt"
	"net"
	"time"

	"github.com/rs/zerolog"
)

type Context struct {
	zc *zerolog.Context
}

func NewContext(logger *zerolog.Logger) *Context {
	zc := logger.With()
	return &Context{&zc}
}

func (context *Context) Logger() *zerolog.Logger {
	logger := context.zc.Logger()
	return &logger
}

func (context *Context) With() *Context {
	zc := context.Logger().With()
	return &Context{&zc}
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
	return c.zc.Logger().GetLevel() <= zerolog.DebugLevel
}

func (c *Context) IsTraceLevel() bool {
	return c.zc.Logger().GetLevel() <= zerolog.TraceLevel
}

func (c *Context) AnErr(key string, err error) *Context {
	nc := c.zc.AnErr(key, err)
	return &Context{zc: &nc}
}

func (c *Context) Any(key string, i interface{}) *Context {
	nc := c.zc.Any(key, i)
	return &Context{zc: &nc}
}

func (c *Context) Array(key string, arr zerolog.LogArrayMarshaler) *Context {
	nc := c.zc.Array(key, arr)
	return &Context{zc: &nc}
}

func (c *Context) Bool(key string, val bool) *Context {
	nc := c.zc.Bool(key, val)
	return &Context{zc: &nc}
}
func (c *Context) Bools(key string, b []bool) *Context {
	nc := c.zc.Bools(key, b)
	return &Context{zc: &nc}
}

func (c *Context) Byte(key string, val byte) *Context {
	nc := c.zc.Bytes(key, []byte{val})
	return &Context{zc: &nc}
}
func (c *Context) Bytes(key string, val []byte) *Context {
	nc := c.zc.Bytes(key, val)
	return &Context{zc: &nc}
}

func (c *Context) Caller() *Context {
	nc := c.zc.CallerWithSkipFrameCount(1)
	return &Context{zc: &nc}
}

func (c *Context) CallerWithSkipFrameCount(skipFrameCount int) *Context {
	nc := c.zc.CallerWithSkipFrameCount(skipFrameCount + 1)
	return &Context{zc: &nc}
}

func (c *Context) Dict(key string, dict *zerolog.Event) *Context {
	nc := c.zc.Dict(key, dict)
	return &Context{zc: &nc}
}

func (c *Context) Dur(key string, d time.Duration) *Context {
	nc := c.zc.Dur(key, d)
	return &Context{zc: &nc}
}
func (c *Context) Durs(key string, d []time.Duration) *Context {
	nc := c.zc.Durs(key, d)
	return &Context{zc: &nc}
}

func (c *Context) EmbedObject(o zerolog.LogObjectMarshaler) *Context {
	nc := c.zc.EmbedObject(o)
	return &Context{zc: &nc}
}

func (c *Context) Err(err error) *Context {
	nc := c.zc.Err(err)
	return &Context{zc: &nc}
}
func (c *Context) Errs(key string, err []error) *Context {
	nc := c.zc.Errs(key, err)
	return &Context{zc: &nc}
}

func (c *Context) Fields(fields []interface{}) *Context {
	nc := c.zc.Fields(fields)
	return &Context{zc: &nc}
}

func (c *Context) Float32(key string, f float32) *Context {
	nc := c.zc.Float32(key, f)
	return &Context{zc: &nc}
}
func (c *Context) Floats32(key string, f []float32) *Context {
	nc := c.zc.Floats32(key, f)
	return &Context{zc: &nc}
}

func (c *Context) Float64(key string, f float64) *Context {
	nc := c.zc.Float64(key, f)
	return &Context{zc: &nc}
}
func (c *Context) Floats64(key string, f []float64) *Context {
	nc := c.zc.Floats64(key, f)
	return &Context{zc: &nc}
}

func (c *Context) Hex(key string, val []byte) *Context {
	nc := c.zc.Hex(key, val)
	return &Context{zc: &nc}
}

func (c *Context) Int(key string, val int) *Context {
	nc := c.zc.Int(key, val)
	return &Context{zc: &nc}
}
func (c *Context) Ints(key string, i []int) *Context {
	nc := c.zc.Ints(key, i)
	return &Context{zc: &nc}
}

func (c *Context) Int8(key string, i int8) *Context {
	nc := c.zc.Int8(key, i)
	return &Context{zc: &nc}
}
func (c *Context) Ints8(key string, i []int8) *Context {
	nc := c.zc.Ints8(key, i)
	return &Context{zc: &nc}
}

func (c *Context) Int16(key string, i int16) *Context {
	nc := c.zc.Int16(key, i)
	return &Context{zc: &nc}
}
func (c *Context) Ints16(key string, i []int16) *Context {
	nc := c.zc.Ints16(key, i)
	return &Context{zc: &nc}
}

func (c *Context) Int32(key string, i int32) *Context {
	nc := c.zc.Int32(key, i)
	return &Context{zc: &nc}
}
func (c *Context) Ints32(key string, i []int32) *Context {
	nc := c.zc.Ints32(key, i)
	return &Context{zc: &nc}
}

func (c *Context) Int64(key string, i int64) *Context {
	nc := c.zc.Int64(key, i)
	return &Context{zc: &nc}
}
func (c *Context) Ints64(key string, i []int64) *Context {
	nc := c.zc.Ints64(key, i)
	return &Context{zc: &nc}
}

func (c *Context) Interface(key string, val any) *Context {
	nc := c.zc.Interface(key, val)
	return &Context{zc: &nc}
}

func (c *Context) IPAddr(key string, ip net.IP) *Context {
	nc := c.zc.IPAddr(key, ip)
	return &Context{zc: &nc}
}

func (c *Context) IPPrefix(key string, pfx net.IPNet) *Context {
	nc := c.zc.IPPrefix(key, pfx)
	return &Context{zc: &nc}
}

func (c *Context) MACAddr(key string, ha net.HardwareAddr) *Context {
	nc := c.zc.MACAddr(key, ha)
	return &Context{zc: &nc}
}

func (c *Context) Object(key string, obj zerolog.LogObjectMarshaler) *Context {
	nc := c.zc.Object(key, obj)
	return &Context{zc: &nc}
}

func (c *Context) RawJSON(key string, b []byte) *Context {
	nc := c.zc.RawJSON(key, b)
	return &Context{zc: &nc}
}

func (c *Context) Reset() *Context {
	nc := c.zc.Reset()
	return &Context{zc: &nc}
}

func (c *Context) Stack() *Context {
	nc := c.zc.Stack()
	return &Context{zc: &nc}
}

func (c *Context) Str(key string, val string) *Context {
	nc := c.zc.Str(key, val)
	return &Context{zc: &nc}
}

func (c *Context) Stringer(key string, val fmt.Stringer) *Context {
	nc := c.zc.Stringer(key, val)
	return &Context{zc: &nc}
}

func (c *Context) Strs(key string, vals []string) *Context {
	nc := c.zc.Strs(key, vals)
	return &Context{zc: &nc}
}

func (c *Context) Time(key string, t time.Time) *Context {
	nc := c.zc.Time(key, t)
	return &Context{zc: &nc}
}
func (c *Context) Times(key string, t []time.Time) *Context {
	nc := c.zc.Times(key, t)
	return &Context{zc: &nc}
}

func (c *Context) Timestamp() *Context {
	nc := c.zc.Timestamp()
	return &Context{zc: &nc}
}

func (c *Context) Type(key string, val interface{}) *Context {
	nc := c.zc.Type(key, val)
	return &Context{zc: &nc}
}

func (c *Context) Uint(key string, i uint) *Context {
	nc := c.zc.Uint(key, i)
	return &Context{zc: &nc}
}
func (c *Context) Uints(key string, i []uint) *Context {
	nc := c.zc.Uints(key, i)
	return &Context{zc: &nc}
}

func (c *Context) Uint8(key string, i uint8) *Context {
	nc := c.zc.Uint8(key, i)
	return &Context{zc: &nc}
}
func (c *Context) Uints8(key string, i []uint8) *Context {
	nc := c.zc.Uints8(key, i)
	return &Context{zc: &nc}
}

func (c *Context) Uint16(key string, i uint16) *Context {
	nc := c.zc.Uint16(key, i)
	return &Context{zc: &nc}
}
func (c *Context) Uints16(key string, i []uint16) *Context {
	nc := c.zc.Uints16(key, i)
	return &Context{zc: &nc}
}

func (c *Context) Uint32(key string, i uint32) *Context {
	nc := c.zc.Uint32(key, i)
	return &Context{zc: &nc}
}
func (c *Context) Uints32(key string, i []uint32) *Context {
	nc := c.zc.Uints32(key, i)
	return &Context{zc: &nc}
}

func (c *Context) Uint64(key string, i uint64) *Context {
	nc := c.zc.Uint64(key, i)
	return &Context{zc: &nc}
}
func (c *Context) Uints64(key string, i []uint64) *Context {
	nc := c.zc.Uints64(key, i)
	return &Context{zc: &nc}
}
