package services

import (
	"bufio"
	"encoding/binary"
	"io"
	"fmt"
	"net"
	"net/rpc"

	descriptor "code.google.com/p/goprotobuf/compiler/descriptor"
	"code.google.com/p/goprotobuf/compiler/generator"
	"code.google.com/p/goprotobuf/proto"

	"github.com/kylelemons/go-rpcgen/services/wire"
)

// TODO: Use io.ReadWriteCloser instead of net.Conn?

// GenerateService is the core of the services package.
// It generates an interface based on the ServiceDescriptorProto and an RPC
// client implementation of the interface as well as three helper functions
// to create the Client and Server necessary to utilize the service over
// RPC.
func (p *Plugin) GenerateService(svc *descriptor.ServiceDescriptorProto) {
	p.imports = true

	name := generator.CamelCase(*svc.Name)

	p.P("// ", name, " is an interface satisfied by the generated client and")
	p.P("// which must be implemented by the object wrapped by the server.")
	p.P("type ", name, " interface {")
	p.In()
	for _, m := range svc.Method {
		method := generator.CamelCase(*m.Name)
		iType := p.ObjectNamed(*m.InputType)
		oType := p.ObjectNamed(*m.OutputType)
		p.P(method, "(in *", p.TypeName(iType), ", out *", p.TypeName(oType), ") error")
	}
	p.Out()
	p.P("}")
	p.P()
	p.P("// internal wrapper for type-safe RPC calling")
	p.P("type rpc", name, "Client struct {")
	p.In()
	p.P("*rpc.Client")
	p.Out()
	p.P("}")
	for _, m := range svc.Method {
		method := generator.CamelCase(*m.Name)
		iType := p.ObjectNamed(*m.InputType)
		oType := p.ObjectNamed(*m.OutputType)
		p.P("func (this rpc", name, "Client) ", method, "(in *", p.TypeName(iType), ", out *", p.TypeName(oType), ") error {")
		p.In()
		p.P(`return this.Call("`, name, ".", method, `", in, out)`)
		p.Out()
		p.P("}")
	}
	p.P()
	p.P("// New", name, "Client returns an *rpc.Client wrapper for calling the methods of")
	p.P("// ", name, " remotely.")
	p.P("func New", name, "Client(conn net.Conn) ", name, " {")
	p.In()
	p.P("return rpc", name, "Client{rpc.NewClientWithCodec(services.NewClientCodec(conn))}")
	p.Out()
	p.P("}")
	p.P()
	p.P("// Serve", name, " serves the given ", name, " backend implementation on conn.")
	p.P("func Serve", name, "(conn net.Conn, backend ", name, ") error {")
	p.In()
	p.P("srv := rpc.NewServer()")
	p.P(`if err := srv.RegisterName("`, name, `", backend); err != nil {`)
	p.In()
	p.P("return err")
	p.Out()
	p.P("}")
	p.P("srv.ServeCodec(services.NewServerCodec(conn))")
	p.P("return nil")
	p.Out()
	p.P("}")
	p.P()
	p.P("// Dial", name, " returns a ", name, " for calling the ", name, " servince at addr (TCP).")
	p.P("func Dial", name, "(addr string) (", name, ", error) {")
	p.In()
	p.P(`conn, err := net.Dial("tcp", addr)`)
	p.P("if err != nil {")
	p.In()
	p.P("return nil, err")
	p.Out()
	p.P("}")
	p.P("return New", name, "Client(conn), nil")
	p.Out()
	p.P("}")
	p.P()
	p.P("// ListenAndServe", name, " serves the given ", name, " backend implementation")
	p.P("// on all connections accepted as a result of listening on addr (TCP).")
	p.P("func ListenAndServe", name, "(addr string, backend ", name, ") error {")
	p.In()
	p.P(`clients, err := net.Listen("tcp", addr)`)
	p.P("if err != nil {")
	p.In()
	p.P("return err")
	p.Out()
	p.P("}")
	p.P("srv := rpc.NewServer()")
	p.P(`if err := srv.RegisterName("`, name, `", backend); err != nil {`)
	p.In()
	p.P("return err")
	p.Out()
	p.P("}")
	p.P("for {")
	p.In()
	p.P("conn, err := clients.Accept()")
	p.P("if err != nil {")
	p.In()
	p.P("return err")
	p.Out()
	p.P("}")
	p.P("go srv.ServeCodec(services.NewServerCodec(conn))")
	p.Out()
	p.P("}")
	p.P(`panic("unreachable")`)
	p.Out()
	p.P("}")
}

// ServerCodec implements the rpc.ServerCodec interface for generic protobufs.
// The same implementation works for all protobufs because it defers the
// decoding of a protocol buffer to the proto package and it uses a set header
// that is the same regardless of the protobuf being used for the RPC.
type ServerCodec struct {
	r *bufio.Reader
	w io.WriteCloser
}

// NewServerCodec returns a ServerCodec that communicates with the ClientCodec
// on the other end of the given conn.
func NewServerCodec(conn net.Conn) *ServerCodec {
	return &ServerCodec{bufio.NewReader(conn), conn}
}

// ReadRequestHeader reads the header protobuf (which is prefixed by a uvarint
// indicating its size) from the connection, decodes it, and stores the fields
// in the given request.
func (s *ServerCodec) ReadRequestHeader(req *rpc.Request) error {
	size, err := binary.ReadUvarint(s.r)
	if err != nil {
		return err
	}
	// TODO max size?
	message := make([]byte, size)
	if _, err := io.ReadFull(s.r, message); err != nil {
		return err
	}
	var header wire.Header
	if err := proto.Unmarshal(message, &header); err != nil {
		return err
	}
	if header.Method == nil {
		return fmt.Errorf("header missing method: %s", header)
	}
	if header.Seq == nil {
		return fmt.Errorf("header missing seq: %s", header)
	}
	req.ServiceMethod = *header.Method
	req.Seq = *header.Seq
	return nil
}

// ReadRequestBody reads a uvarint from the connection and decodes that many
// subsequent bytes into the given protobuf (which should be a pointer to a
// struct that is generated by the proto package).
func (s *ServerCodec) ReadRequestBody(pb interface{}) error {
	size, err := binary.ReadUvarint(s.r)
	if err != nil {
		return err
	}
	// TODO max size?
	message := make([]byte, size)
	if _, err := io.ReadFull(s.r, message); err != nil {
		return err
	}
	return proto.Unmarshal(message, pb)
}

// WriteResponse writes the appropriate header protobuf and the given protobuf
// to the connection (each prefixed with a uvarint indicating its size).  If
// the response was invalid, the size of the body of the resp is reported as
// having size zero and is not sent.
func (s *ServerCodec) WriteResponse(resp *rpc.Response, pb interface{}) error {
	var header wire.Header
	var size []byte
	var data []byte
	var err error

	// Allocate enough space for the biggest size
	size = make([]byte, binary.MaxVarintLen64)

	// Write the header
	if resp.Error != "" {
		header.Error = &resp.Error
	}
	header.Method = &resp.ServiceMethod
	header.Seq = &resp.Seq
	if data, err = proto.Marshal(&header); err != nil {
		return err
	}
	size = size[:binary.PutUvarint(size, uint64(len(data)))]
	if _, err = s.w.Write(size); err != nil {
		return err
	}
	if _, err = s.w.Write(data); err != nil {
		return err
	}

	// Write the proto
	size = size[:cap(size)]
	if _, invalid := pb.(rpc.InvalidRequest); invalid {
		data = nil
	} else {
		if data, err = proto.Marshal(pb); err != nil {
			return err
		}
	}
	size = size[:binary.PutUvarint(size, uint64(len(data)))]
	if _, err = s.w.Write(size); err != nil {
		return err
	}
	if _, err = s.w.Write(data); err != nil {
		return err
	}

	// All done
	return nil
}

// Close closes the underlying conneciton.
func (s *ServerCodec) Close() error {
	return s.w.Close()
}

// ClientCodec implements the rpc.ClientCodec interface for generic protobufs.
// The same implementation works for all protobufs because it defers the
// encoding of a protocol buffer to the proto package and it uses a set header
// that is the same regardless of the protobuf being used for the RPC.
type ClientCodec struct {
	r *bufio.Reader
	w io.WriteCloser
}

// NewClientCodec returns a ClientCodec for communicating with the ServerCodec
// on the other end of the conn.
func NewClientCodec(conn net.Conn) *ClientCodec {
	return &ClientCodec{bufio.NewReader(conn), conn}
}

// WriteRequest writes the appropriate header protobuf and the given protobuf
// to the connection (each prefixed with a uvarint indicating its size).
func (c *ClientCodec) WriteRequest(req *rpc.Request, pb interface{}) error {
	var header wire.Header
	var size []byte
	var data []byte
	var err error

	// Allocate enough space for the biggest size
	size = make([]byte, binary.MaxVarintLen64)

	// Write the header
	header.Method = &req.ServiceMethod
	header.Seq = &req.Seq
	if data, err = proto.Marshal(&header); err != nil {
		return err
	}
	size = size[:binary.PutUvarint(size, uint64(len(data)))]
	if _, err = c.w.Write(size); err != nil {
		return err
	}
	if _, err = c.w.Write(data); err != nil {
		return err
	}

	// Write the proto
	size = size[:cap(size)]
	if data, err = proto.Marshal(pb); err != nil {
		return err
	}
	size = size[:binary.PutUvarint(size, uint64(len(data)))]
	if _, err = c.w.Write(size); err != nil {
		return err
	}
	if _, err = c.w.Write(data); err != nil {
		return err
	}

	// All done
	return nil
}

// ReadResponseHeader reads the header protobuf (which is prefixed by a uvarint
// indicating its size) from the connection, decodes it, and stores the fields
// in the given request.
func (c *ClientCodec) ReadResponseHeader(resp *rpc.Response) error {
	size, err := binary.ReadUvarint(c.r)
	if err != nil {
		return err
	}
	// TODO max size?
	message := make([]byte, size)
	if _, err := io.ReadFull(c.r, message); err != nil {
		return err
	}
	var header wire.Header
	if err := proto.Unmarshal(message, &header); err != nil {
		return err
	}
	if header.Method == nil {
		return fmt.Errorf("header missing method: %s", header)
	}
	if header.Seq == nil {
		return fmt.Errorf("header missing seq: %s", header)
	}
	resp.ServiceMethod = *header.Method
	resp.Seq = *header.Seq
	if header.Error != nil {
		resp.Error = *header.Error
	}
	return nil
}

// ReadResponseBody reads a uvarint from the connection and decodes that many
// subsequent bytes into the given protobuf (which should be a pointer to a
// struct that is generated by the proto package).  If the uvarint size read
// is zero, nothing is done (this indicates an error condition, which was
// encapsulated in the header)
func (c *ClientCodec) ReadResponseBody(pb interface{}) error {
	size, err := binary.ReadUvarint(c.r)
	if err != nil {
		return err
	}
	if size == 0 || pb == nil {
		return nil
	}

	// TODO max size?
	message := make([]byte, size)
	if _, err := io.ReadFull(c.r, message); err != nil {
		return err
	}
	return proto.Unmarshal(message, pb)
}

// Close closes the underlying connection.
func (c *ClientCodec) Close() error {
	return c.w.Close()
}

// BUG: The server/client don't do a sanity check on the size of the proto
// before reading it, so it's possible to maliciously instruct the
// client/server to allocate too much memory.
