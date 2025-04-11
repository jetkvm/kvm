package kvm

import (
	"context"
	"os"
	"sync"
	"syscall"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
)

type WebRTCStreamFile struct {
	fs.Inode
	mu   sync.Mutex
	Attr fuse.Attr
	size uint64
}

var _ = (fs.NodeOpener)((*WebRTCStreamFile)(nil))
var _ = (fs.NodeOpener)((*WebRTCStreamFile)(nil))
var _ = (fs.NodeOpener)((*WebRTCStreamFile)(nil))
var _ = (fs.NodeOpener)((*WebRTCStreamFile)(nil))
var _ = (fs.NodeOpener)((*WebRTCStreamFile)(nil))

func (f *WebRTCStreamFile) Open(ctx context.Context, flags uint32) (fh fs.FileHandle, fuseFlags uint32, errno syscall.Errno) {
	return nil, fuse.FOPEN_KEEP_CACHE, fs.OK
}

func (f *WebRTCStreamFile) Write(ctx context.Context, fh fs.FileHandle, data []byte, off int64) (uint32, syscall.Errno) {
	return 0, syscall.EROFS
}

var _ = (fs.NodeGetattrer)((*WebRTCStreamFile)(nil))

func (f *WebRTCStreamFile) Getattr(ctx context.Context, fh fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	f.mu.Lock()
	defer f.mu.Unlock()
	out.Attr = f.Attr
	out.Size = f.size
	return fs.OK
}

func (f *WebRTCStreamFile) Setattr(ctx context.Context, fh fs.FileHandle, in *fuse.SetAttrIn, out *fuse.AttrOut) syscall.Errno {
	f.mu.Lock()
	defer f.mu.Unlock()
	out.Attr = f.Attr
	return fs.OK
}

func (f *WebRTCStreamFile) Flush(ctx context.Context, fh fs.FileHandle) syscall.Errno {
	return fs.OK
}

type DiskReadRequest struct {
	Start uint64 `json:"start"`
	End   uint64 `json:"end"`
}

var diskReadChan = make(chan []byte, 1)

func (f *WebRTCStreamFile) Read(ctx context.Context, fh fs.FileHandle, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	buf, err := webRTCDiskReader.Read(ctx, off, int64(len(dest)))
	if err != nil {
		return nil, syscall.EIO
	}
	return fuse.ReadResultData(buf), fs.OK
}

func (f *WebRTCStreamFile) SetSize(size uint64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.size = size
}

type FuseRoot struct {
	fs.Inode
}

var webRTCStreamFile = &WebRTCStreamFile{}

func (r *FuseRoot) OnAdd(ctx context.Context) {
	ch := r.NewPersistentInode(ctx, webRTCStreamFile, fs.StableAttr{Ino: 2})
	r.AddChild("disk", ch, false)
}

func (r *FuseRoot) Getattr(ctx context.Context, fh fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	out.Mode = 0755
	return 0
}

var _ = (fs.NodeGetattrer)((*FuseRoot)(nil))
var _ = (fs.NodeOnAdder)((*FuseRoot)(nil))

const fuseMountPoint = "/mnt/webrtc"

var fuseServer *fuse.Server

func RunFuseServer() {
	opts := &fs.Options{}
	opts.DirectMountStrict = true
	_ = os.Mkdir(fuseMountPoint, 0755)
	var err error
	fuseServer, err = fs.Mount(fuseMountPoint, &FuseRoot{}, opts)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to mount fuse")
	}
	fuseServer.Wait()
}

type WebRTCImage struct {
	Size     uint64 `json:"size"`
	Filename string `json:"filename"`
}
