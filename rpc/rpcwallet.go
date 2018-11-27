package rpc

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"github.com/copernet/copernicus/log"
	"github.com/copernet/copernicus/logic/lwallet"
	"github.com/copernet/copernicus/model/chain"
	"github.com/copernet/copernicus/model/script"
	"github.com/copernet/copernicus/model/tx"
	"github.com/copernet/copernicus/model/wallet"
	"github.com/copernet/copernicus/rpc/btcjson"
	"github.com/copernet/copernicus/util"
	"github.com/copernet/copernicus/util/amount"
	"github.com/copernet/copernicus/util/cashaddr"
	"github.com/pkg/errors"
	"gopkg.in/fatih/set.v0"
	"strconv"
)

var walletHandlers = map[string]commandHandler{
	"getnewaddress":      handleGetNewAddress,
	"listunspent":        handleListUnspent,
	"settxfee":           handleSetTxFee,
	"sendtoaddress":      handleSendToAddress,
	"getbalance":         handleGetBalance,
	"gettransaction":     handleGetTransaction,
	"sendmany":           handleSendMany,
	"addmultisigaddress": handleAddMultiSigAddress,
	"fundrawtransaction": handleFundRawTransaction,
}

var walletDisableRPCError = &btcjson.RPCError{
	Code:    btcjson.ErrRPCMethodNotFound.Code,
	Message: "Method not found (wallet method is disabled because no wallet is loaded)",
}

func handleGetNewAddress(s *Server, cmd interface{}, closeChan <-chan struct{}) (interface{}, error) {
	if !lwallet.IsWalletEnable() {
		return nil, walletDisableRPCError
	}

	c := cmd.(*btcjson.GetNewAddressCmd)

	account := *c.Account
	address, err := lwallet.GetNewAddress(account, false)
	if err != nil {
		log.Info("GetNewAddress error:%s", err.Error())
		return nil, btcjson.ErrRPCInternal
	}

	return address, nil
}

func handleListUnspent(s *Server, cmd interface{}, closeChan <-chan struct{}) (interface{}, error) {
	if !lwallet.IsWalletEnable() {
		return nil, walletDisableRPCError
	}

	c := cmd.(*btcjson.ListUnspentCmd)

	minDepth := *c.MinConf
	maxDepth := *c.MaxConf
	includeUnsafe := *c.IncludeUnsafe
	addresses := make(map[string]string)
	if c.Addresses != nil {
		for _, address := range *c.Addresses {
			_, keyHash, rpcErr := decodeAddress(address)
			if rpcErr != nil {
				return nil, rpcErr
			}
			if _, ok := addresses[string(keyHash)]; ok {
				return nil, btcjson.NewRPCError(btcjson.ErrRPCInvalidParameter,
					"Invalid parameter, duplicated address: "+address)
			}
			addresses[string(keyHash)] = address
		}
	}

	results := make([]*btcjson.ListUnspentResult, 0)

	coins := lwallet.AvailableCoins(!includeUnsafe, true)
	for _, txnCoin := range coins {
		depth := int32(0)
		if !txnCoin.Coin.IsMempoolCoin() {
			depth = chain.GetInstance().Height() - txnCoin.Coin.GetHeight() + 1
		}
		if depth < minDepth || depth > maxDepth {
			continue
		}
		scriptPubKey := txnCoin.Coin.GetScriptPubKey()
		scriptType, scriptAddresses, _, err := scriptPubKey.ExtractDestinations()
		if err != nil || len(scriptAddresses) != 1 {
			continue
		}
		keyHash := scriptAddresses[0].EncodeToPubKeyHash()

		var address string
		if len(addresses) > 0 {
			var ok bool
			if address, ok = addresses[string(keyHash)]; !ok {
				continue
			}
		} else {
			address = scriptAddresses[0].String()
		}
		unspentInfo := &btcjson.ListUnspentResult{
			TxID:          txnCoin.OutPoint.Hash.String(),
			Vout:          txnCoin.OutPoint.Index,
			Address:       address,
			ScriptPubKey:  hex.EncodeToString(scriptPubKey.Bytes()),
			Amount:        valueFromAmount(int64(txnCoin.Coin.GetAmount())),
			Confirmations: depth,
			Spendable:     true, //TODO
			Solvable:      true, //TODO
			Safe:          txnCoin.IsSafe,
		}

		if account := lwallet.GetAccountName(keyHash); account != "" {
			unspentInfo.Account = account
		}
		if scriptType == script.ScriptHash {
			if redeemScript := lwallet.GetScript(keyHash); redeemScript != nil {
				scriptHexString := hex.EncodeToString(redeemScript.Bytes())
				unspentInfo.RedeemScript = scriptHexString
			}
		}
		results = append(results, unspentInfo)
	}
	return results, nil
}

func handleSetTxFee(s *Server, cmd interface{}, closeChan <-chan struct{}) (interface{}, error) {
	if !lwallet.IsWalletEnable() {
		return nil, walletDisableRPCError
	}

	c := cmd.(*btcjson.SetTxFeeCmd)

	feePaid, rpcErr := amountFromValue(c.Amount)
	if rpcErr != nil {
		return false, rpcErr
	}

	lwallet.SetFeeRate(int64(feePaid), 1000)

	return true, nil
}

func handleSendToAddress(s *Server, cmd interface{}, closeChan <-chan struct{}) (interface{}, error) {
	if !lwallet.IsWalletEnable() {
		return nil, walletDisableRPCError
	}

	c := cmd.(*btcjson.SendToAddressCmd)

	scriptPubKey, rpcErr := getStandardScriptPubKey(c.Address, nil)
	if rpcErr != nil {
		return nil, rpcErr
	}

	// Amount
	value, rpcErr := amountFromValue(c.Amount)
	if rpcErr != nil {
		return false, rpcErr
	}

	// Wallet comments
	extInfo := make(map[string]string)
	if c.Comment != nil {
		extInfo["comment"] = *c.Comment
	}
	if c.CommentTo != nil {
		extInfo["to"] = *c.CommentTo
	}

	subtractFeeFromAmount := *c.SubtractFeeFromAmount

	txn, rpcErr := sendMoney(scriptPubKey, value, subtractFeeFromAmount, extInfo)
	if rpcErr != nil {
		return false, rpcErr
	}
	txHash := txn.GetHash()
	return txHash.String(), nil
}

func handleGetBalance(s *Server, cmd interface{}, closeChan <-chan struct{}) (interface{}, error) {
	if !lwallet.IsWalletEnable() {
		return nil, walletDisableRPCError
	}
	//TODO add Confirmation
	balance := wallet.GetInstance().GetBalance()

	return balance.ToBTC(), nil
}
func handleGetTransaction(s *Server, cmd interface{}, closeChan <-chan struct{}) (interface{}, error) {
	if !lwallet.IsWalletEnable() {
		return nil, walletDisableRPCError
	}
	c := cmd.(*btcjson.GetTransactionCmd)
	pwallet := wallet.GetInstance()
	txHash, err := util.GetHashFromStr(c.Txid)
	if err != nil {
		return nil, errors.New("Tx Hash is err")
	}
	wtx := pwallet.GetWalletTx(*txHash)
	if wtx == nil {
		return nil, errors.New("Invalid or non-wallet transaction id")
	}

	ret := &btcjson.GetTransactionResult{}
	filter := wallet.ISMINE_SPENDABLE
	credit := wtx.GetCredit(filter)
	debit := wtx.GetDebit(filter)
	net := credit - debit
	var fee amount.Amount
	if debit > 0 {
		fee = wtx.Tx.GetValueOut() - debit
	}

	// Fill GetTransactionResult
	ret.Amount = (net - fee).ToBTC()
	if debit > 0 {
		ret.Fee = fee.ToBTC()
	}
	ret.Confirmations = wtx.GetDepthInMainChain()
	if ret.Confirmations > 0 {
		index := chain.GetInstance().GetIndex(wtx.GetBlokHeight())
		ret.BlockHash = index.GetBlockHash().String()
		ret.BlockTime = index.GetBlockTime()
	}
	ret.TxID = c.Txid
	ret.WalletConflicts = nil
	ret.TimeReceived = wtx.TimeReceived

	buf := bytes.NewBuffer(nil)
	if err := wtx.Tx.Serialize(buf); err != nil {
		return nil, rpcDecodeHexError(c.Txid)
	}
	strHex := hex.EncodeToString(buf.Bytes())
	ret.Hex = strHex

	// Fill GetTransactionDetailsResult

	return ret, nil
}
func handleFundRawTransaction(s *Server, cmd interface{}, closeChan <-chan struct{}) (interface{}, error) {
	if !lwallet.IsWalletEnable() {
		return nil, walletDisableRPCError
	}
	c := cmd.(*btcjson.FundRawTransactionCmd)
	b, _ := hex.DecodeString(c.HexTx)
	ubuf := bytes.NewBuffer(b)
	txn := tx.Tx{}
	if err := txn.Unserialize(ubuf); err != nil {
		return nil, rpcDecodeHexError(c.HexTx)
	}

	if txn.GetOutsCount() == 0 {
		return nil, btcjson.NewRPCError(btcjson.RPCInvalidParameter, "TX must have at least one output")
	}
	setSubtractFeeFromOutputs := set.New()
	if c.Options == nil {
		c.Options = &btcjson.FundRawTxoptions{
			IncludeWatching:  false,
			LockUnspents:     false,
			ReserveChangeKey: true,
			ChangePosition:   0,
		}
	} else {
		changePosition := c.Options.ChangePosition
		if changePosition != -1 && (changePosition < 0 || changePosition > txn.GetOutsCount()) {
			return nil, btcjson.NewRPCError(btcjson.RPCInvalidParameter, "changePosition out of bounds")
		}

		subtractFeeFromOutputs := *c.Options.SubtractFeeFromOutputs
		for _, pos := range subtractFeeFromOutputs {
			if setSubtractFeeFromOutputs.Has(pos) {
				return nil, btcjson.NewRPCError(btcjson.RPCInvalidParameter,
					fmt.Sprintf("Invalid parameter, duplicated position: %d", pos))
			}
			if pos < 0 {
				return nil, btcjson.NewRPCError(btcjson.RPCInvalidParameter,
					fmt.Sprintf("Invalid parameter, duplicated position: %d", pos))
			}
			if pos >= txn.GetOutsCount() {
				return nil, btcjson.NewRPCError(btcjson.RPCInvalidParameter,
					fmt.Sprintf("Invalid parameter, position too large: %d", pos))
			}
			setSubtractFeeFromOutputs.Add(pos)
		}
	}
	pos, feeOut, err := lwallet.FundTransaction(&txn, setSubtractFeeFromOutputs, c.Options)
	if err != nil {
		return nil, btcjson.NewRPCError(btcjson.ErrRPCWallet, err.Error())
	}

	sbuf := bytes.NewBuffer(nil)
	if err := txn.Serialize(sbuf); err != nil {
		log.Error("rawTransaction:serialize tx failed: %v", err)
		return nil, btcjson.NewRPCError(btcjson.ErrRPCWallet, err.Error())
	}
	return &btcjson.FundRawTransactionResult{
		Hex:       hex.EncodeToString(sbuf.Bytes()),
		Changepos: pos,
		Fee:       feeOut.ToBTC(),
	}, nil
}

func sendMoney(scriptPubKey *script.Script, value amount.Amount, subtractFeeFromAmount bool,
	extInfo map[string]string) (*tx.Tx, *btcjson.RPCError) {

	curBalance := wallet.GetInstance().GetBalance()

	// Check amount
	if value <= 0 {
		return nil, btcjson.NewRPCError(btcjson.RPCInvalidParameter, "Invalid amount")
	}
	if value > curBalance {
		return nil, btcjson.NewRPCError(btcjson.RPCWalletInsufficientFunds, "Insufficient funds")
	}

	// TODO: check Peer-to-peer connection

	// Create and send the transaction
	recipients := make([]*wallet.Recipient, 1)
	recipients[0] = &wallet.Recipient{
		ScriptPubKey:          scriptPubKey,
		Value:                 value,
		SubtractFeeFromAmount: subtractFeeFromAmount,
	}
	changePosRet := -1
	txn, feeRequired, err := lwallet.CreateTransaction(recipients, &changePosRet, true)
	if err != nil {
		if !subtractFeeFromAmount && value+feeRequired > curBalance {
			errMsg := fmt.Sprintf("Error: This transaction requires a "+
				"transaction fee of at least %s", feeRequired.String())
			err = errors.New(errMsg)
		}
		return nil, btcjson.NewRPCError(btcjson.RPCWalletError, err.Error())
	}

	err = lwallet.CommitTransaction(txn, extInfo)
	if err != nil {
		errMsg := "Error: The transaction was rejected! Reason given: " + err.Error()
		return nil, btcjson.NewRPCError(btcjson.RPCWalletError, errMsg)
	}
	return txn, nil
}

func handleSendMany(s *Server, cmd interface{}, closeChan <-chan struct{}) (interface{}, error) {
	if !lwallet.IsWalletEnable() {
		return nil, walletDisableRPCError
	}

	c := cmd.(*btcjson.SendManyCmd)

	// TODO: check Peer-to-peer connection

	strAccount := c.FromAccount
	if strAccount == "*" {
		return nil, btcjson.NewRPCError(btcjson.RPCWalletInvalidAccountName, "Invalid account anme")
	}

	sendTo := c.Amounts

	if *c.MinConf < 0 {
		*c.MinConf = 1
	}

	walletTx := wallet.NewEmptyWalletTx()
	extInfo := make(map[string]string)
	walletTx.ExtInfo = extInfo
	walletTx.FromAccount = strAccount

	if c.Comment != nil {
		walletTx.ExtInfo["comment"] = *c.Comment
	}

	var recipients []*wallet.Recipient

	var totalAmount amount.Amount
	for key, value := range sendTo {
		address := key
		money := value
		// TODO: Destination check

		scriptPubKey, rpcErr := getStandardScriptPubKey(address, nil)
		if rpcErr != nil {
			return nil, rpcErr
		}
		amount, rpcErr := amountFromValue(money)
		if rpcErr != nil {
			return nil, rpcErr
		}

		if amount < 0 {
			return nil, btcjson.NewRPCError(btcjson.RPCTypeError, "Invalid amount for send")
		}

		totalAmount += amount

		subTractFeeFromeAmount := false
		if c.SubTractFeeFrom != nil {
			for idx := 0; idx < len(*c.SubTractFeeFrom); idx++ {
				addr := (*c.SubTractFeeFrom)[idx]
				if addr == address {
					subTractFeeFromeAmount = true
				}
			}
		}

		reciptient := &wallet.Recipient{
			ScriptPubKey:          scriptPubKey,
			Value:                 amount,
			SubtractFeeFromAmount: subTractFeeFromeAmount,
		}
		recipients = append(recipients, reciptient)
	}

	// Check funds
	// TODO: GetLeagacybalance
	balance := wallet.GetInstance().GetBalance()
	if totalAmount > balance {
		return nil, btcjson.NewRPCError(btcjson.RPCWalletInsufficientFunds, "Account has insufficient funds")
	}

	changePosRet := -1
	txn, feeRequired, err := lwallet.CreateTransaction(recipients, &changePosRet, true)
	if err != nil || feeRequired+totalAmount > balance {
		return nil, btcjson.NewRPCError(btcjson.RPCWalletInsufficientFunds, err.Error())
	}

	err = lwallet.CommitTransaction(txn, walletTx.ExtInfo)
	if err != nil {
		errMsg := "Error: The transaction was rejected! Reason given: " + err.Error()
		return nil, btcjson.NewRPCError(btcjson.RPCWalletError, errMsg)
	}

	return txn.GetHash().String(), nil
}

func handleAddMultiSigAddress(s *Server, cmd interface{}, closeChan <-chan struct{}) (interface{}, error) {
	if !lwallet.IsWalletEnable() {
		return nil, walletDisableRPCError
	}
	c := cmd.(*btcjson.AddMultiSigAddressCmd)
	num := c.RequiredNum
	keys := c.Keys
	// Gather public keys
	if num < 1 {
		return nil, btcjson.NewRPCError(btcjson.RPCInvalidParameter,
			"a multisignature address must require at least one key to redeem")
	}
	if len(keys) < num {
		return nil, btcjson.NewRPCError(btcjson.RPCInvalidParameter,
			"not enough keys supplied (got "+strconv.Itoa(len(keys))+" keys, "+
				"but need at least "+strconv.Itoa(num)+"to redeem")
	}
	if len(keys) > 16 {
		return nil, btcjson.NewRPCError(btcjson.RPCInvalidParameter,
			"Number of addresses involved in the multisignature address creation > 16\nReduce the number")
	}
	inner, err := lwallet.CreateMultiSigRedeemScript(num, keys)
	if err != nil {
		return nil, btcjson.NewRPCError(btcjson.RPCInvalidParameter, err.Error())
	}

	innerHash := util.Hash160(inner.GetData())
	addr, err := cashaddr.NewCashAddressScriptHashFromHash(innerHash, chain.GetInstance().GetParams())
	if err != nil {
		return nil, btcjson.NewRPCError(btcjson.RPCInvalidParameter, err.Error())
	}

	pwallet := wallet.GetInstance()
	pwallet.AddScript(inner)
	pwallet.SetAddressBook(innerHash, "", "send")
	return addr.String(), nil

}

func registerWalletRPCCommands() {
	for name, handler := range walletHandlers {
		appendCommand(name, handler)
	}
}
