// Copyright 2018 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package gitprotocolio

import (
	"bytes"
	"fmt"
	"io"
	"strings"
)

type protocolV2RequestState int

const (
	protocolV2RequestStateBegin protocolV2RequestState = iota
	protocolV2RequestStateScanCapabilities
	protocolV2RequestStateScanArguments
	protocolV2RequestStateEnd
)

// ProtocolV2RequestChunk is a chunk of a protocol v2 request.
type ProtocolV2RequestChunk struct {
	Command       string
	Capability    string
	EndCapability bool
	Argument      []byte
	EndArgument   bool
	EndRequest    bool
}

// EncodeToPktLine serializes the chunk.
func (c *ProtocolV2RequestChunk) EncodeToPktLine() []byte {
	if c.Command != "" {
		return BytesPacket([]byte(fmt.Sprintf("command=%s\n", c.Command))).EncodeToPktLine()
	}
	if c.Capability != "" {
		return BytesPacket([]byte(c.Capability + "\n")).EncodeToPktLine()
	}
	if c.EndCapability {
		return DelimPacket{}.EncodeToPktLine()
	}
	if len(c.Argument) != 0 {
		return BytesPacket(c.Argument).EncodeToPktLine()
	}
	if c.EndArgument || c.EndRequest {
		return FlushPacket{}.EncodeToPktLine()
	}
	panic("impossible chunk")
}

// ProtocolV2Request provides an interface for reading a protocol v2 request.
type ProtocolV2Request struct {
	scanner *PacketScanner
	state   protocolV2RequestState
	err     error
	curr    *ProtocolV2RequestChunk
}

// NewProtocolV2Request returns a new ProtocolV2Request to read from rd.
func NewProtocolV2Request(rd io.Reader) *ProtocolV2Request {
	return &ProtocolV2Request{scanner: NewPacketScanner(rd)}
}

// Err returns the first non-EOF error that was encountered by the
// ProtocolV2Request.
func (r *ProtocolV2Request) Err() error {
	return r.err
}

// Chunk returns the most recent request chunk generated by a call to Scan.
//
// The underlying array of Argument may point to data that will be overwritten
// by a subsequent call to Scan. It does no allocation.
func (r *ProtocolV2Request) Chunk() *ProtocolV2RequestChunk {
	return r.curr
}

// Scan advances the scanner to the next packet. It returns false when the scan
// stops, either by reaching the end of the input or an error. After scan
// returns false, the Err method will return any error that occurred during
// scanning, except that if it was io.EOF, Err will return nil.
func (r *ProtocolV2Request) Scan() bool {
	if r.err != nil || r.state == protocolV2RequestStateEnd {
		return false
	}
	if !r.scanner.Scan() {
		r.err = r.scanner.Err()
		if r.err == nil && r.state != protocolV2RequestStateBegin {
			r.err = SyntaxError("early EOF")
		}
		return false
	}
	pkt := r.scanner.Packet()

	switch r.state {
	case protocolV2RequestStateBegin:
		switch p := pkt.(type) {
		case FlushPacket:
			r.state = protocolV2RequestStateEnd
			r.curr = &ProtocolV2RequestChunk{
				EndRequest: true,
			}
			return true
		case BytesPacket:
			if !bytes.HasPrefix(p, []byte("command=")) {
				r.err = SyntaxError(fmt.Sprintf("unexpected packet: %#v", p))
				return false
			}
			r.state = protocolV2RequestStateScanCapabilities
			r.curr = &ProtocolV2RequestChunk{
				Command: strings.TrimSuffix(strings.TrimPrefix(string(p), "command="), "\n"),
			}
			return true
		default:
			r.err = SyntaxError(fmt.Sprintf("unexpected packet: %#v", p))
			return false
		}
	case protocolV2RequestStateScanCapabilities:
		switch p := pkt.(type) {
		case DelimPacket:
			r.state = protocolV2RequestStateScanArguments
			r.curr = &ProtocolV2RequestChunk{
				EndCapability: true,
			}
			return true
		case BytesPacket:
			r.curr = &ProtocolV2RequestChunk{
				Capability: strings.TrimSuffix(string(p), "\n"),
			}
			return true
		default:
			r.err = SyntaxError(fmt.Sprintf("unexpected packet: %#v", p))
			return false
		}
	case protocolV2RequestStateScanArguments:
		switch p := pkt.(type) {
		case FlushPacket:
			r.state = protocolV2RequestStateBegin
			r.curr = &ProtocolV2RequestChunk{
				EndArgument: true,
			}
			return true
		case BytesPacket:
			r.curr = &ProtocolV2RequestChunk{
				Argument: p,
			}
			return true
		default:
			r.err = SyntaxError(fmt.Sprintf("unexpected packet: %#v", p))
			return false
		}
	}
	panic("impossible state")
}