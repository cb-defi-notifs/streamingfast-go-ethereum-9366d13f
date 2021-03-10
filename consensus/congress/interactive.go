package congress

import (
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/deepmind"
	"github.com/ethereum/go-ethereum/rlp"
	"golang.org/x/crypto/sha3"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
)

type chainContext struct {
	chainReader consensus.ChainHeaderReader
	engine      consensus.Engine
}

func newChainContext(chainReader consensus.ChainHeaderReader, engine consensus.Engine) *chainContext {
	return &chainContext{
		chainReader: chainReader,
		engine:      engine,
	}
}

// Engine retrieves the chain's consensus engine.
func (cc *chainContext) Engine() consensus.Engine {
	return cc.engine
}

// GetHeader returns the hash corresponding to their hash.
func (cc *chainContext) GetHeader(hash common.Hash, number uint64) *types.Header {
	return cc.chainReader.GetHeader(hash, number)
}

func getInteractiveABI() map[string]abi.ABI {
	abiMap := make(map[string]abi.ABI, 0)
	tmpABI, _ := abi.JSON(strings.NewReader(validatorsInteractiveABI))
	abiMap[validatorsContractName] = tmpABI
	tmpABI, _ = abi.JSON(strings.NewReader(punishInteractiveABI))
	abiMap[punishContractName] = tmpABI
	tmpABI, _ = abi.JSON(strings.NewReader(proposalInteractiveABI))
	abiMap[proposalContractName] = tmpABI

	return abiMap
}

// executeMsg executes transaction sent to system contracts.
func executeMsg(msg core.Message, state *state.StateDB, header *types.Header, chainContext core.ChainContext, chainConfig *params.ChainConfig, dmContext *deepmind.Context) (ret []byte, err error) {
	var txHash common.Hash
	if dmContext.Enabled() {
		sha := sha3.NewLegacyKeccak256().(crypto.KeccakState)
		sha.Reset()

		if err := rlp.Encode(sha, []interface{}{header.Number.Uint64(), msg}); err != nil {
			return nil, err
		}
		if _, err := sha.Read(txHash[:]); err != nil {
			return nil, err
		}

		dmContext.StartTransactionRaw(
			txHash,
			msg.To(),
			msg.Value(),
			new(big.Int).Bytes(), new(big.Int).Bytes(), new(big.Int).Bytes(),
			msg.Gas(),
			msg.GasPrice(),
			msg.Nonce(),
			msg.Data(),
		)
		dmContext.RecordTrxFrom(msg.From())
	}

	// Set gas price to zero
	context := core.NewEVMContext(msg, header, chainContext, nil)
	vmenv := vm.NewEVM(context, state, chainConfig, vm.Config{}, dmContext)

	ret, leftOverGas, err := vmenv.Call(vm.AccountRef(msg.From()), *msg.To(), msg.Data(), msg.Gas(), msg.Value())

	if err != nil {
		return []byte{}, err
	}

	if dmContext.Enabled() {
		gasUsed := msg.Gas() - leftOverGas
		cumulativeGasUsed := dmContext.CumulativeGasUsed() + gasUsed

		//TODO: What to put in this Receipt
		receipt := types.NewReceipt(nil, err != nil, cumulativeGasUsed)
		receipt.TxHash = txHash
		receipt.GasUsed = msg.Gas() - leftOverGas

		// if the transaction created a contract, store the creation address in the receipt.
		if msg.To() == nil {
			receipt.ContractAddress = crypto.CreateAddress(vmenv.Context.Origin, header.Number.Uint64())
		}
		// Set the receipt logs and create a bloom for filtering
		receipt.Logs = state.GetLogs(txHash)
		receipt.Bloom = types.CreateBloom(types.Receipts{receipt})
		receipt.BlockHash = header.Hash()
		receipt.BlockNumber = header.Number
		receipt.TransactionIndex = dmContext.LastTransactionIndex() + 1
		dmContext.EndTransaction(receipt)
	}

	return ret, nil
}
