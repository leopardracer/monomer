package integration_test

import (
	"math/big"
	"testing"

	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/log"
	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil/integration"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	"github.com/cosmos/cosmos-sdk/x/auth"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	authsims "github.com/cosmos/cosmos-sdk/x/auth/simulation"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/bank"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/ethereum/go-ethereum/common"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/polymerdao/monomer"
	monomertestutils "github.com/polymerdao/monomer/testutils"
	"github.com/polymerdao/monomer/x/rollup"
	rollupkeeper "github.com/polymerdao/monomer/x/rollup/keeper"
	rolluptypes "github.com/polymerdao/monomer/x/rollup/types"
	"github.com/stretchr/testify/require"
)

const initialFeeCollectorBalance = 1_000_000

func TestRollup(t *testing.T) {
	integrationApp, feeCollectorAddr := setupIntegrationApp(t)
	queryClient := banktypes.NewQueryClient(integrationApp.QueryHelper())

	erc20tokenAddr := common.HexToAddress("0xabcdef123456")
	erc20userAddr := common.HexToAddress("0x123456abcdef")
	erc20depositAmount := big.NewInt(100)

	l1AttributesTx, depositTx, _ := monomertestutils.GenerateEthTxs(t)
	erc20DepositTx := monomertestutils.GenerateERC20DepositTx(t, erc20tokenAddr, erc20userAddr, erc20depositAmount)
	l1WithdrawalAddr := common.HexToAddress("0x112233445566").String()

	l1AttributesTxBz := monomertestutils.TxToBytes(t, l1AttributesTx)
	depositTxBz := monomertestutils.TxToBytes(t, depositTx)
	erc20DepositTxBz := monomertestutils.TxToBytes(t, erc20DepositTx)

	mintAmount := depositTx.Mint()
	transferAmount := depositTx.Value()
	from, err := gethtypes.NewCancunSigner(depositTx.ChainId()).Sender(depositTx)
	require.NoError(t, err)

	mintAddr, err := monomer.CosmosETHAddress(from).Encode(sdk.GetConfig().GetBech32AccountAddrPrefix())
	require.NoError(t, err)
	userCosmosAddr, err := monomer.CosmosETHAddress(*depositTx.To()).Encode(sdk.GetConfig().GetBech32AccountAddrPrefix())
	require.NoError(t, err)

	// query the mint address ETH balance and assert it's zero
	require.Equal(t, math.ZeroInt(), queryETHBalance(t, queryClient, mintAddr, integrationApp))

	// query the recipient address ETH balance and assert it's zero
	require.Equal(t, math.ZeroInt(), queryETHBalance(t, queryClient, userCosmosAddr, integrationApp))

	// query the user's ERC20 balance and assert it's zero
	erc20userCosmosAddr, err := monomer.CosmosETHAddress(erc20userAddr).Encode(sdk.GetConfig().GetBech32AccountAddrPrefix())
	require.NoError(t, err)
	require.Equal(t, math.ZeroInt(), queryERC20Balance(t, queryClient, erc20userCosmosAddr, erc20tokenAddr, integrationApp))

	// send an invalid MsgApplyL1Txs and assert error
	_, err = integrationApp.RunMsg(&rolluptypes.MsgApplyL1Txs{
		TxBytes: [][]byte{l1AttributesTxBz, l1AttributesTxBz},
	})
	require.Error(t, err)

	// send a successful MsgApplyL1Txs and mint ETH to user
	_, err = integrationApp.RunMsg(&rolluptypes.MsgApplyL1Txs{
		TxBytes: [][]byte{l1AttributesTxBz, depositTxBz, erc20DepositTxBz},
	})
	require.NoError(t, err)

	// query the mint address ETH balance and assert it's equal to the mint amount minus the transfer amount
	require.Equal(t, new(big.Int).Sub(mintAmount, transferAmount), queryETHBalance(t, queryClient, mintAddr, integrationApp).BigInt())

	// query the recipient address ETH balance and assert it's equal to the transfer amount
	require.Equal(t, transferAmount, queryETHBalance(t, queryClient, userCosmosAddr, integrationApp).BigInt())

	// query the user's ERC20 balance and assert it's equal to the deposit amount
	require.Equal(t, erc20depositAmount, queryERC20Balance(t, queryClient, erc20userCosmosAddr, erc20tokenAddr, integrationApp).BigInt())

	// try to withdraw more than deposited and assert error
	_, err = integrationApp.RunMsg(&rolluptypes.MsgInitiateWithdrawal{
		Sender:   mintAddr,
		Target:   l1WithdrawalAddr,
		Value:    math.NewIntFromBigInt(transferAmount).Add(math.OneInt()),
		GasLimit: new(big.Int).SetUint64(100_000_000).Bytes(),
		Data:     []byte{0x01, 0x02, 0x03},
	})
	require.Error(t, err)

	// send a successful MsgInitiateWithdrawal
	_, err = integrationApp.RunMsg(&rolluptypes.MsgInitiateWithdrawal{
		Sender:   userCosmosAddr,
		Target:   l1WithdrawalAddr,
		Value:    math.NewIntFromBigInt(transferAmount),
		GasLimit: new(big.Int).SetUint64(100_000_000).Bytes(),
		Data:     []byte{0x01, 0x02, 0x03},
	})
	require.NoError(t, err)

	// query the recipient address ETH balance and assert it's zero
	require.Equal(t, math.ZeroInt(), queryETHBalance(t, queryClient, userCosmosAddr, integrationApp))

	// query the fee collector's ETH balance and assert it's equal to the initial mint amount
	require.Equal(t, math.NewInt(initialFeeCollectorBalance), queryETHBalance(t, queryClient, feeCollectorAddr, integrationApp))

	// send a successful MsgInitiateFeeWithdrawal
	_, err = integrationApp.RunMsg(&rolluptypes.MsgInitiateFeeWithdrawal{
		Sender: userCosmosAddr,
	})
	require.NoError(t, err)

	// query the fee collector's ETH balance and assert it's zero
	require.Equal(t, math.ZeroInt(), queryETHBalance(t, queryClient, feeCollectorAddr, integrationApp))
}

func setupIntegrationApp(t *testing.T) (*integration.App, string) {
	encodingCfg := moduletestutil.MakeTestEncodingConfig(auth.AppModuleBasic{}, bank.AppModuleBasic{}, rollup.AppModuleBasic{})
	keys := storetypes.NewKVStoreKeys(authtypes.StoreKey, banktypes.StoreKey, rolluptypes.StoreKey)
	authority := authtypes.NewModuleAddress("gov").String()

	logger := log.NewTestLogger(t)

	cms := integration.CreateMultiStore(keys, logger)

	accountKeeper := authkeeper.NewAccountKeeper(
		encodingCfg.Codec,
		runtime.NewKVStoreService(keys[authtypes.StoreKey]),
		authtypes.ProtoBaseAccount,
		map[string][]string{
			authtypes.FeeCollectorName: {},
			rolluptypes.ModuleName:     {authtypes.Minter, authtypes.Burner},
		},
		addresscodec.NewBech32Codec("cosmos"),
		"cosmos",
		authority,
	)
	bankKeeper := bankkeeper.NewBaseKeeper(
		encodingCfg.Codec,
		runtime.NewKVStoreService(keys[banktypes.StoreKey]),
		accountKeeper,
		map[string]bool{},
		authority,
		logger,
	)
	rollupKeeper := rollupkeeper.NewKeeper(
		encodingCfg.Codec,
		runtime.NewKVStoreService(keys[rolluptypes.StoreKey]),
		bankKeeper,
		accountKeeper,
	)

	authModule := auth.NewAppModule(encodingCfg.Codec, accountKeeper, authsims.RandomGenesisAccounts, nil)
	bankModule := bank.NewAppModule(encodingCfg.Codec, bankKeeper, accountKeeper, nil)
	rollupModule := rollup.NewAppModule(encodingCfg.Codec, rollupKeeper)

	// Start the integration test with funds in the fee collector account since fees are disabled in the simulated integration app
	ctx := sdk.NewContext(cms, cmtproto.Header{}, false, logger)
	initialFees := sdk.NewCoins(sdk.NewCoin(rolluptypes.WEI, math.NewInt(initialFeeCollectorBalance)))
	require.NoError(t, bankKeeper.MintCoins(ctx, rolluptypes.ModuleName, initialFees))
	require.NoError(t, bankKeeper.SendCoinsFromModuleToModule(ctx, rolluptypes.ModuleName, authtypes.FeeCollectorName, initialFees))

	integrationApp := integration.NewIntegrationApp(
		ctx,
		logger,
		keys,
		encodingCfg.Codec,
		map[string]appmodule.AppModule{
			authtypes.ModuleName:   authModule,
			banktypes.ModuleName:   bankModule,
			rolluptypes.ModuleName: rollupModule,
		},
	)
	rolluptypes.RegisterMsgServer(integrationApp.MsgServiceRouter(), rollupKeeper)
	banktypes.RegisterQueryServer(integrationApp.QueryHelper(), bankkeeper.NewQuerier(&bankKeeper))

	return integrationApp, accountKeeper.GetModuleAddress(authtypes.FeeCollectorName).String()
}

func queryBalance(t *testing.T, queryClient banktypes.QueryClient, addr, denom string, app *integration.App) math.Int {
	resp, err := queryClient.Balance(app.Context(), &banktypes.QueryBalanceRequest{
		Address: addr,
		Denom:   denom,
	})
	require.NoError(t, err)
	return resp.Balance.Amount
}

func queryETHBalance(t *testing.T, queryClient banktypes.QueryClient, addr string, app *integration.App) math.Int {
	return queryBalance(t, queryClient, addr, rolluptypes.WEI, app)
}

func queryERC20Balance(t *testing.T, queryClient banktypes.QueryClient, addr string, erc20addr common.Address, app *integration.App) math.Int {
	return queryBalance(t, queryClient, addr, "erc20/"+erc20addr.String()[2:], app)
}
