package ltx

import (
	"github.com/copernet/copernicus/model/outpoint"
	"github.com/copernet/copernicus/model/tx"
	"github.com/copernet/copernicus/model/undo"
	"github.com/copernet/copernicus/model/utxo"
)

func TxSpendCoins(tx *tx.Tx, coinMap *utxo.CoinsMap, txundo *undo.TxUndo) {
	if !tx.IsCoinBase() {
		undoCoins := make([]*utxo.Coin, len(tx.GetIns()))
		for idx, txin := range tx.GetIns() {
			coin := coinMap.AccessCoin(txin.PreviousOutPoint)
			undoCoins[idx] = coin.DeepCopy()
			coinMap.SpendCoin(txin.PreviousOutPoint)
		}
		if txundo != nil {
			txundo.SetUndoCoins(undoCoins)
		}
	}
}

func TxAddCoins(tx *tx.Tx, coinMap *utxo.CoinsMap, height int32) {
	isCoinbase := tx.IsCoinBase()
	txid := tx.GetHash()
	for idx, out := range tx.GetOuts() {
		op := outpoint.NewOutPoint(txid, uint32(idx))
		coin := utxo.NewFreshCoin(out, height, isCoinbase)
		coinMap.AddCoin(op, coin, isCoinbase)
	}
}
