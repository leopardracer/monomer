package integrations

import (
	"context"
	"io"
	stdURL "net/url"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"cosmossdk.io/log"
	"cosmossdk.io/store/snapshots"
	storetypes "cosmossdk.io/store/types"
	abcitypes "github.com/cometbft/cometbft/abci/types"
	bftclient "github.com/cometbft/cometbft/rpc/client/http"
	bfttypes "github.com/cometbft/cometbft/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/server/api"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/cosmos/gogoproto/grpc"
	"github.com/polymerdao/monomer/e2e/url"
	"github.com/polymerdao/monomer/testapp"
	"github.com/sourcegraph/conc"
	"github.com/stretchr/testify/require"
)

// Application Constructor `appCreator` for testing
func mockAppCreator(
	_ log.Logger,
	db dbm.DB,
	_ io.Writer,
	_ servertypes.AppOptions,
) servertypes.Application {
	app, err := testapp.New(db, "1")
	if err != nil {
		panic(err)
	}

	return &WrappedTestApp{App: app}
}

// Unit test for Monomer's custom `StartCommandHandler` callback
func TestStartCommandHandler(t *testing.T) {
	svrCtx := server.NewDefaultContext()
	svrCtx.Config.RootDir = t.TempDir()
	svrCtx.Config.DBBackend = "memdb"
	genesisPath, err := filepath.Abs("testdata/genesis.json")
	require.NoError(t, err)
	svrCtx.Config.Genesis = genesisPath

	// This flag must be set, because by default it's set to ""
	svrCtx.Viper.Set("minimum-gas-prices", "0.025stake")
	// Disable gRPC server (enabled by default)
	svrCtx.Viper.Set("grpc.enable", false)
	svrCtx.Viper.Set(flagEngineURL, "ws://127.0.0.1:9000")

	clientCtx := client.Context{}
	inProcessConsensus := true
	opts := server.StartCmdOptions{
		DBOpener: func(rootDir string, backendType dbm.BackendType) (dbm.DB, error) {
			return dbm.NewMemDB(), nil
		},
	}

	cmtListenAddr := svrCtx.Config.RPC.ListenAddress
	cmtListenAddr = strings.TrimPrefix(cmtListenAddr, "tcp://")
	cmtListenURL, err := url.Parse(&stdURL.URL{Scheme: "http", Host: cmtListenAddr})
	require.NoError(t, err)

	var wg conc.WaitGroup
	defer wg.Wait()
	wg.Go(func() {
		err := startCommandHandler(svrCtx, clientCtx, mockAppCreator, inProcessConsensus, opts)
		require.NoError(t, err)
	})

	ctx := context.Background()
	require.True(t, cmtListenURL.IsReachable(ctx))

	// --- Submit a Monomer Tx ---
	bftClient, err := bftclient.New("http://"+cmtListenAddr, "http://"+cmtListenAddr)
	require.NoError(t, err, "could not create CometBFT client")
	t.Log("CometBFT client created", "bftClient", bftClient)

	txBytes := testapp.ToTestTx(t, "userTxKey", "userTxValue")
	bftTx := bfttypes.Tx(txBytes)

	putTx, err := bftClient.BroadcastTxAsync(ctx, txBytes)
	require.NoError(t, err, "could not broadcast tx")
	require.Equal(t, abcitypes.CodeTypeOK, putTx.Code, "put.Code is not OK")
	require.EqualValues(t, bftTx.Hash(), putTx.Hash, "put.Hash is not equal to bftTx.Hash")
	t.Log("Monomer Tx broadcasted successfully", "txHash", putTx.Hash)

	// TODO: expose gRPC and API server in the integrated testapp and assert endpoints behave as expected

	sigCh <- syscall.SIGINT
}

// Wrapper around `testapp.App` to satisfy the `ABCI` and `servertypes.Application` interfaces
type WrappedTestApp struct {
	*testapp.App
}

var _ servertypes.ABCI = (*WrappedTestApp)(nil)

func (w *WrappedTestApp) Info(r *abcitypes.RequestInfo) (*abcitypes.ResponseInfo, error) {
	return w.App.Info(context.TODO(), r)
}

func (w *WrappedTestApp) InitChain(r *abcitypes.RequestInitChain) (*abcitypes.ResponseInitChain, error) {
	return w.App.InitChain(context.TODO(), r)
}

func (w *WrappedTestApp) CheckTx(r *abcitypes.RequestCheckTx) (*abcitypes.ResponseCheckTx, error) {
	return w.App.CheckTx(context.TODO(), r)
}

func (w *WrappedTestApp) Commit() (*abcitypes.ResponseCommit, error) {
	return w.App.Commit(context.TODO(), nil)
}

func (w *WrappedTestApp) FinalizeBlock(r *abcitypes.RequestFinalizeBlock) (*abcitypes.ResponseFinalizeBlock, error) {
	return w.App.FinalizeBlock(context.TODO(), r)
}

func (w *WrappedTestApp) ListSnapshots(_ *abcitypes.RequestListSnapshots) (*abcitypes.ResponseListSnapshots, error) {
	panic("not implemented")
}

func (w *WrappedTestApp) OfferSnapshot(_ *abcitypes.RequestOfferSnapshot) (*abcitypes.ResponseOfferSnapshot, error) {
	panic("not implemented")
}

func (w *WrappedTestApp) LoadSnapshotChunk(_ *abcitypes.RequestLoadSnapshotChunk) (*abcitypes.ResponseLoadSnapshotChunk, error) {
	panic("not implemented")
}

func (w *WrappedTestApp) ApplySnapshotChunk(_ *abcitypes.RequestApplySnapshotChunk) (*abcitypes.ResponseApplySnapshotChunk, error) {
	panic("not implemented")
}

func (w *WrappedTestApp) PrepareProposal(_ *abcitypes.RequestPrepareProposal) (*abcitypes.ResponsePrepareProposal, error) {
	panic("not implemented")
}

func (w *WrappedTestApp) ProcessProposal(_ *abcitypes.RequestProcessProposal) (*abcitypes.ResponseProcessProposal, error) {
	panic("not implemented")
}

func (w *WrappedTestApp) ExtendVote(_ context.Context, _ *abcitypes.RequestExtendVote) (*abcitypes.ResponseExtendVote, error) {
	panic("not implemented")
}

func (w *WrappedTestApp) VerifyVoteExtension(_ *abcitypes.RequestVerifyVoteExtension) (*abcitypes.ResponseVerifyVoteExtension, error) {
	panic("not implemented")
}

var _ servertypes.Application = (*WrappedTestApp)(nil)

func (w *WrappedTestApp) RegisterAPIRoutes(*api.Server, serverconfig.APIConfig) {
	panic("not implemented")
}

func (w *WrappedTestApp) RegisterGRPCServer(grpc.Server) {
	panic("not implemented")
}

func (w *WrappedTestApp) RegisterTxService(_ client.Context) { //nolint:gocritic // hugeParam
	panic("not implemented")
}

func (w *WrappedTestApp) RegisterTendermintService(_ client.Context) { //nolint:gocritic // hugeParam
	panic("not implemented")
}

func (w *WrappedTestApp) RegisterNodeService(_ client.Context, _ serverconfig.Config) { //nolint:gocritic // hugeParam
	panic("not implemented")
}

func (w *WrappedTestApp) SnapshotManager() *snapshots.Manager {
	panic("not implemented")
}

func (w *WrappedTestApp) Close() error {
	return nil
}

func (w *WrappedTestApp) CommitMultiStore() storetypes.CommitMultiStore {
	panic("not implemented")
}
