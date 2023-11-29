package tctx

import (
	dsclient "github.com/henderiw/apiserver-runtime-example/pkg/dataserver/client"
	sdcpb "github.com/iptecharch/sdc-protos/sdcpb"
)

type Context struct {
	DataStore *sdcpb.CreateDataStoreRequest
	Client    dsclient.Client
}
