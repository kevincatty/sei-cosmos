package mock

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/types"

	bam "github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec"
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// NewApp creates a simple mock kvstore app for testing. It should work
// similar to a real app. Make sure rootDir is empty before running the test,
// in order to guarantee consistent results
func NewApp(rootDir string, logger log.Logger) (abci.Application, error) {
	db, err := sdk.NewLevelDB("mock", filepath.Join(rootDir, "data"))
	if err != nil {
		return nil, err
	}

	// Capabilities key to access the main KVStore.
	capKeyMainStore := sdk.NewKVStoreKey("main")

	// Create BaseApp.
	baseApp := bam.NewBaseApp("kvstore", logger, db, decodeTx, nil, &testutil.TestAppOpts{})

	// Set mounts for BaseApp's MultiStore.
	baseApp.MountStores(capKeyMainStore)

	baseApp.SetInitChainer(InitChainer(capKeyMainStore))
	baseApp.SetFinalizeBlocker(func(ctx sdk.Context, req *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
		txResults := []*abci.ExecTxResult{}
		for _, txbz := range req.Txs {
			tx, err := decodeTx(txbz)
			if err != nil {
				txResults = append(txResults, &abci.ExecTxResult{})
				continue
			}
			deliverTxResp := baseApp.DeliverTx(ctx, abci.RequestDeliverTx{
				Tx: txbz,
			}, tx, sha256.Sum256(txbz))
			txResults = append(txResults, &abci.ExecTxResult{
				Code:      deliverTxResp.Code,
				Data:      deliverTxResp.Data,
				Log:       deliverTxResp.Log,
				Info:      deliverTxResp.Info,
				GasWanted: deliverTxResp.GasWanted,
				GasUsed:   deliverTxResp.GasUsed,
				Events:    deliverTxResp.Events,
				Codespace: deliverTxResp.Codespace,
			})
		}
		return &abci.ResponseFinalizeBlock{
			TxResults: txResults,
		}, nil
	})

	baseApp.Router().AddRoute(sdk.NewRoute("kvstore", KVStoreHandler(capKeyMainStore)))

	// Load latest version.
	if err := baseApp.LoadLatestVersion(); err != nil {
		return nil, err
	}

	return baseApp, nil
}

// KVStoreHandler is a simple handler that takes kvstoreTx and writes
// them to the db
func KVStoreHandler(storeKey sdk.StoreKey) sdk.Handler {
	return func(ctx sdk.Context, msg sdk.Msg) (*sdk.Result, error) {
		dTx, ok := msg.(kvstoreTx)
		if !ok {
			return nil, errors.New("KVStoreHandler should only receive kvstoreTx")
		}

		// tx is already unmarshalled
		key := dTx.key
		value := dTx.value

		store := ctx.KVStore(storeKey)
		store.Set(key, value)

		return &sdk.Result{
			Log: fmt.Sprintf("set %s=%s", key, value),
		}, nil
	}
}

// basic KV structure
type KV struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// What Genesis JSON is formatted as
type GenesisJSON struct {
	Values []KV `json:"values"`
}

// InitChainer returns a function that can initialize the chain
// with key/value pairs
func InitChainer(key sdk.StoreKey) func(sdk.Context, abci.RequestInitChain) abci.ResponseInitChain {
	return func(ctx sdk.Context, req abci.RequestInitChain) abci.ResponseInitChain {
		stateJSON := req.AppStateBytes

		genesisState := new(GenesisJSON)
		err := json.Unmarshal(stateJSON, genesisState)
		if err != nil {
			panic(err) // TODO https://github.com/cosmos/cosmos-sdk/issues/468
			// return sdk.ErrGenesisParse("").TraceCause(err, "")
		}

		for _, val := range genesisState.Values {
			store := ctx.KVStore(key)
			store.Set([]byte(val.Key), []byte(val.Value))
		}
		return abci.ResponseInitChain{}
	}
}

// AppGenState can be passed into InitCmd, returns a static string of a few
// key-values that can be parsed by InitChainer
func AppGenState(_ *codec.LegacyAmino, _ types.GenesisDoc, _ []json.RawMessage) (appState json.
	RawMessage, err error) {
	appState = json.RawMessage(`{
  "values": [
    {
        "key": "hello",
        "value": "goodbye"
    },
    {
        "key": "foo",
        "value": "bar"
    }
  ]
}`)
	return
}

// AppGenStateEmpty returns an empty transaction state for mocking.
func AppGenStateEmpty(_ *codec.LegacyAmino, _ types.GenesisDoc, _ []json.RawMessage) (
	appState json.RawMessage, err error) {
	appState = json.RawMessage(``)
	return
}

// Manually write the handlers for this custom message
type MsgServer interface {
	Test(ctx context.Context, msg *kvstoreTx) (*sdk.Result, error)
}

type MsgServerImpl struct {
	capKeyMainStore *storetypes.KVStoreKey
}

func (m MsgServerImpl) Test(ctx context.Context, msg *kvstoreTx) (*sdk.Result, error) {
	return KVStoreHandler(m.capKeyMainStore)(sdk.UnwrapSDKContext(ctx), msg)
}
