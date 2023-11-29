package client

import (
	"context"
	"fmt"
	"time"

	"github.com/henderiw/logger/log"
	sdcpb "github.com/iptecharch/sdc-protos/sdcpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Client interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context)
	GetAddress() string
	sdcpb.DataServerClient
}

type Config struct {
	Address    string
	Username   string
	Password   string
	Proxy      bool
	NoTLS      bool
	TLSCA      string
	TLSCert    string
	TLSKey     string
	SkipVerify bool
	Insecure   bool
	MaxMsgSize int
}

func New(cfg *Config) (Client, error) {
	if cfg == nil {
		return nil, fmt.Errorf("cannot create client with empty config")
	}
	if cfg.Address == "" {
		return nil, fmt.Errorf("cannot create client with an empty address")
	}

	return &client{
		cfg: cfg,
	}, nil
}

type client struct {
	cfg      *Config
	cancel   context.CancelFunc
	conn     *grpc.ClientConn
	dsclient sdcpb.DataServerClient
}

func (r *client) Stop(ctx context.Context) {
	log := log.FromContext(ctx)
	log.Info("stopping...")
	r.conn.Close()
	r.cancel()
}

func (r *client) Start(ctx context.Context) error {
	log := log.FromContext(ctx)
	log.Info("starting...")

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var err error
	r.conn, err = grpc.DialContext(ctx, r.cfg.Address,
		grpc.WithBlock(),
		grpc.WithTransportCredentials(
			insecure.NewCredentials(),
		),
	)
	if err != nil {
		return err
	}

	//defer conn.Close()
	r.dsclient = sdcpb.NewDataServerClient(r.conn)
	log.Info("started...")
	go func() {
		for {
			select {
			case <-ctx.Done():
				log.Info("stopped...")
				return
			}
		}
	}()
	return nil
}

func (r *client) GetAddress() string {
	return r.cfg.Address
}

func (r *client) ListDataStore(ctx context.Context, in *sdcpb.ListDataStoreRequest, opts ...grpc.CallOption) (*sdcpb.ListDataStoreResponse, error) {
	return r.dsclient.ListDataStore(ctx, in, opts...)
}

func (r *client) GetDataStore(ctx context.Context, in *sdcpb.GetDataStoreRequest, opts ...grpc.CallOption) (*sdcpb.GetDataStoreResponse, error) {
	return r.dsclient.GetDataStore(ctx, in, opts...)
}

func (r *client) CreateDataStore(ctx context.Context, in *sdcpb.CreateDataStoreRequest, opts ...grpc.CallOption) (*sdcpb.CreateDataStoreResponse, error) {
	return r.dsclient.CreateDataStore(ctx, in, opts...)
}

func (r *client) DeleteDataStore(ctx context.Context, in *sdcpb.DeleteDataStoreRequest, opts ...grpc.CallOption) (*sdcpb.DeleteDataStoreResponse, error) {
	return r.dsclient.DeleteDataStore(ctx, in, opts...)
}

func (r *client) Commit(ctx context.Context, in *sdcpb.CommitRequest, opts ...grpc.CallOption) (*sdcpb.CommitResponse, error) {
	return r.dsclient.Commit(ctx, in, opts...)
}

func (r *client) Rebase(ctx context.Context, in *sdcpb.RebaseRequest, opts ...grpc.CallOption) (*sdcpb.RebaseResponse, error) {
	return r.dsclient.Rebase(ctx, in, opts...)
}

func (r *client) Discard(ctx context.Context, in *sdcpb.DiscardRequest, opts ...grpc.CallOption) (*sdcpb.DiscardResponse, error) {
	return r.dsclient.Discard(ctx, in, opts...)
}

func (r *client) GetData(ctx context.Context, in *sdcpb.GetDataRequest, opts ...grpc.CallOption) (sdcpb.DataServer_GetDataClient, error) {
	return r.dsclient.GetData(ctx, in, opts...)
}

func (r *client) SetData(ctx context.Context, in *sdcpb.SetDataRequest, opts ...grpc.CallOption) (*sdcpb.SetDataResponse, error) {
	return r.dsclient.SetData(ctx, in, opts...)
}

func (r *client) Diff(ctx context.Context, in *sdcpb.DiffRequest, opts ...grpc.CallOption) (*sdcpb.DiffResponse, error) {
	return r.dsclient.Diff(ctx, in, opts...)
}

func (r *client) Subscribe(ctx context.Context, in *sdcpb.SubscribeRequest, opts ...grpc.CallOption) (sdcpb.DataServer_SubscribeClient, error) {
	return r.dsclient.Subscribe(ctx, in, opts...)
}

func (r *client) Watch(ctx context.Context, in *sdcpb.WatchRequest, opts ...grpc.CallOption) (sdcpb.DataServer_WatchClient, error) {
	return r.dsclient.Watch(ctx, in, opts...)
}

/*
func (r *client) getGRPCOpts() ([]grpc.DialOption, error) {
	var opts []grpc.DialOption
	fmt.Printf("grpc client config: %v\n", r.cfg)
	if r.cfg.Insecure {
		//opts = append(opts, grpc.WithInsecure())
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	} else {
		tlsConfig, err := r.newTLS()
		if err != nil {
			return nil, err
		}
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)))
	}
	return opts, nil
}

func (r *client) newTLS() (*tls.Config, error) {
	tlsConfig := &tls.Config{
		Renegotiation:      tls.RenegotiateNever,
		InsecureSkipVerify: r.cfg.SkipVerify,
	}
	//err := loadCerts(tlsConfig)
	//if err != nil {
	//	return nil, err
	//}
	return tlsConfig, nil
}

func loadCerts(tlscfg *tls.Config) error {
	if c.TLSCert != "" && c.TLSKey != "" {
		certificate, err := tls.LoadX509KeyPair(*c.TLSCert, *c.TLSKey)
		if err != nil {
			return err
		}
		tlscfg.Certificates = []tls.Certificate{certificate}
		tlscfg.BuildNameToCertificate()
	}
	if c.TLSCA != nil && *c.TLSCA != "" {
		certPool := x509.NewCertPool()
		caFile, err := ioutil.ReadFile(*c.TLSCA)
		if err != nil {
			return err
		}
		if ok := certPool.AppendCertsFromPEM(caFile); !ok {
			return errors.New("failed to append certificate")
		}
		tlscfg.RootCAs = certPool
	}
	return nil
}
*/
