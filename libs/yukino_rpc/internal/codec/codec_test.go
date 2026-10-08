package codec

import (
	"bytes"
	"errors"
	"testing"

	"google.golang.org/protobuf/types/known/emptypb"
)

type samplePayload struct {
	Name string
	Size int
}

type failingCompressor struct{}

func (f failingCompressor) compress([]byte) ([]byte, error) {
	return nil, errors.New("compress failed")
}

func (f failingCompressor) decompress([]byte) ([]byte, error) {
	return nil, errors.New("decompress failed")
}

func TestJSONCodecRoundTrip(t *testing.T) {
	cc, err := New(JSON)
	if err != nil {
		t.Fatalf("New(JSON) returned error: %v", err)
	}
	input := samplePayload{Name: "rpc", Size: 7}
	data, err := cc.Marshal(input)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	var output samplePayload
	if err := cc.Unmarshal(data, &output); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if output != input {
		t.Fatalf("output = %+v, want %+v", output, input)
	}
}

func TestProtoCodec(t *testing.T) {
	cc, err := New(PROTO)
	if err != nil {
		t.Fatalf("New(PROTO) returned error: %v", err)
	}
	if _, err := cc.Marshal(samplePayload{}); err == nil {
		t.Fatal("expected marshal error for non-proto payload")
	}
	if err := cc.Unmarshal(nil, &samplePayload{}); err == nil {
		t.Fatal("expected unmarshal error for non-proto payload")
	}
	data, err := cc.Marshal(&emptypb.Empty{})
	if err != nil {
		t.Fatalf("Marshal proto returned error: %v", err)
	}
	var output emptypb.Empty
	if err := cc.Unmarshal(data, &output); err != nil {
		t.Fatalf("Unmarshal proto returned error: %v", err)
	}
}

func TestCodecRegistryErrorsAndPanics(t *testing.T) {
	if _, err := New(Type(99)); err == nil {
		t.Fatal("expected unknown codec error")
	}
	assertPanics(t, func() { Register(Type(100), nil) })
	Register(Type(101), func() Codec { return &jSONCodec{} })
	assertPanics(t, func() { Register(Type(101), func() Codec { return &jSONCodec{} }) })
}

func TestCompression(t *testing.T) {
	input := bytes.Repeat([]byte("payload"), 32)
	compressed, err := Compress(input, CompressionGzip)
	if err != nil {
		t.Fatalf("Compress returned error: %v", err)
	}
	if bytes.Equal(compressed, input) {
		t.Fatal("gzip output should differ from input")
	}
	output, err := Decompress(compressed, CompressionGzip)
	if err != nil {
		t.Fatalf("Decompress returned error: %v", err)
	}
	if !bytes.Equal(output, input) {
		t.Fatalf("output = %q, want %q", output, input)
	}
	if _, err := Compress(input, CompressionType(99)); err == nil {
		t.Fatal("expected missing compressor error")
	}
	if _, err := Decompress([]byte("bad gzip"), CompressionGzip); err == nil {
		t.Fatal("expected invalid gzip error")
	}
	RegisterCompressor(CompressionType(88), failingCompressor{})
	if _, err := Compress(input, CompressionType(88)); err == nil {
		t.Fatal("expected compressor failure")
	}
	if _, err := Decompress(input, CompressionType(88)); err == nil {
		t.Fatal("expected decompressor failure")
	}
}

func assertPanics(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	fn()
}
