package timinglog

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/rtp"
)

func at(us int64) time.Time {
	return epoch.Add(time.Duration(us) * time.Microsecond)
}

func answerJSON(t *testing.T, action string) string {
	t.Helper()
	out, err := json.Marshal(Answer(action))
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestAnswerIsTheLogInTheFormatOfTheTools(t *testing.T) {
	t.Cleanup(Stop)

	Key(4, at(500))
	Frame(90000, at(900))
	if got, want := answerJSON(t, "read"), `{"running":false,"keyCount":0,"frameCount":0,"keys":[],"frames":[]}`; got != want {
		t.Errorf("before the start:\n got %s\nwant %s", got, want)
	}

	if got, want := answerJSON(t, "start"), `{"running":true,"keyCount":0,"frameCount":0,"keys":[],"frames":[]}`; got != want {
		t.Errorf("start:\n got %s\nwant %s", got, want)
	}
	Key(4, at(1000))
	Key(0, at(2500))
	Frame(4000000000, at(3000))
	want := `{"running":true,"keyCount":2,"frameCount":1,"keys":[[1000,4],[2500,0]],"frames":[[3000,4000000000]]}`
	if got := answerJSON(t, "read"); got != want {
		t.Errorf("read:\n got %s\nwant %s", got, want)
	}

	if got, want := answerJSON(t, "start"), `{"running":true,"keyCount":0,"frameCount":0,"keys":[],"frames":[]}`; got != want {
		t.Errorf("a second start:\n got %s\nwant %s", got, want)
	}
	Key(5, at(4000))
	if got, want := answerJSON(t, "stop"), `{"running":false,"keyCount":0,"frameCount":0,"keys":[],"frames":[]}`; got != want {
		t.Errorf("stop:\n got %s\nwant %s", got, want)
	}
	Key(6, at(5000))
	if got := Read(); got.Running || got.KeyCount != 0 || len(got.Keys) != 0 {
		t.Errorf("after the stop: got %+v, want no entries", got)
	}
}

func TestTheLogKeepsTheNewestEntriesAndCountsAll(t *testing.T) {
	t.Cleanup(Stop)
	Start()
	const total = frameCapacity + 300
	for i := range total {
		Key(byte(i), at(int64(i)))
		Frame(uint32(i)*1500, at(int64(i)))
	}
	log := Read()
	if log.KeyCount != total || log.FrameCount != total {
		t.Errorf("counts: got %d keys and %d frames, want %d of each", log.KeyCount, log.FrameCount, total)
	}
	if len(log.Keys) != keyCapacity || len(log.Frames) != frameCapacity {
		t.Fatalf("entries: got %d keys and %d frames, want %d and %d", len(log.Keys), len(log.Frames), keyCapacity, frameCapacity)
	}
	firstKey, firstFrame := int64(total-keyCapacity), int64(total-frameCapacity)
	if log.Keys[0] != [2]int64{firstKey, firstKey % 256} || log.Keys[keyCapacity-1][0] != total-1 {
		t.Errorf("keys: got %v to %v, want the times %d to %d", log.Keys[0], log.Keys[keyCapacity-1], firstKey, total-1)
	}
	if log.Frames[0] != [2]int64{firstFrame, firstFrame * 1500} || log.Frames[frameCapacity-1][0] != total-1 {
		t.Errorf("frames: got %v to %v, want the times %d to %d", log.Frames[0], log.Frames[frameCapacity-1], firstFrame, total-1)
	}
}

func TestVideoTapReadsTheTimestampOfVideoPacketsOnly(t *testing.T) {
	tap := &VideoTap{}
	var written []uint32
	next := interceptor.RTPWriterFunc(func(header *rtp.Header, payload []byte, _ interceptor.Attributes) (int, error) {
		written = append(written, header.Timestamp)
		return len(payload), nil
	})
	video := tap.BindLocalStream(&interceptor.StreamInfo{MimeType: "video/H264"}, next)
	audio := tap.BindLocalStream(&interceptor.StreamInfo{MimeType: "audio/opus"}, next)

	if _, packets := tap.Last(); packets != 0 {
		t.Fatalf("before a packet: got %d packets", packets)
	}
	for _, timestamp := range []uint32{3000, 3000, 6000} {
		if n, err := video.Write(&rtp.Header{Timestamp: timestamp}, []byte{1, 2}, nil); n != 2 || err != nil {
			t.Fatalf("video write: got %d, %v", n, err)
		}
	}
	if _, err := audio.Write(&rtp.Header{Timestamp: 777}, []byte{1}, nil); err != nil {
		t.Fatal(err)
	}
	if timestamp, packets := tap.Last(); timestamp != 6000 || packets != 3 {
		t.Errorf("got timestamp %d after %d packets, want 6000 after 3", timestamp, packets)
	}
	if want := []uint32{3000, 3000, 6000, 777}; len(written) != len(want) {
		t.Errorf("the next writer got %v, want %v", written, want)
	}
}
