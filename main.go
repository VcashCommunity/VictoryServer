package main

import (
	"fmt"
	"log"
	"github.com/dgraph-io/badger"
	xvc "github.com/devmahno/vcashrpcgo"
	"time"
	"strconv"
	"path/filepath"
	"os"
)

func main() {
	defer func() {
		// Recover from panic if one occured. Set err to nil otherwise.
		if recover() != nil {
		}
	}()
	start := time.Now()

	// create ini file
	// create log file, write info about transactions
	// parse only incoming transactions, ignore outcoming, store however last transaction id for both

	// Prepare db folder
	dbPath := filepath.Join(".", "db")
	os.MkdirAll(dbPath, os.ModePerm)

	opts := badger.DefaultOptions
	opts.Dir = "db/badger"
	opts.ValueDir = "db/badger"
	db, err := badger.Open(opts)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	fmt.Printf("\n* Call rpc getinfo\n")
	show_data(xvc.RpcGetInfo())

	fmt.Printf("\n* Call rpc getbalance\n")
	show_data(xvc.RpcGetBalance())

	err = db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte("address"))
		if err != nil {
			return err
		}
		val, err := item.Value()
		if err != nil {
			return err
		}
		fmt.Printf("The address is: %s\n", val)
		return nil
	})

	response := xvc.RpcListReceivedByAddress()
	for _, u := range response["result"].([]interface{}) {
		uu := u.(map[string]interface{})
		address := uu["address"].(string)
		amount := uu["amount"].(float64)

		key_found := false
		set_key := false
		// Start a writable transaction.
		txn := db.NewTransaction(false)
		item, err := txn.Get([]byte(address))
		if err != nil {
			fmt.Printf("%s\n", err)
		} else {
			key_found = true
		}

		if key_found {
			val, err := item.Value()
			if err != nil {
				fmt.Printf("%s\n", err)
			}

			var val_amount float64
			val_amount, err = strconv.ParseFloat(string(val[:]), 64)
			if err != nil {
				fmt.Printf("%s\n", err)
				continue
			}
			// Compare values
			if amount != val_amount {
				set_key = true
			}
		} else {
			set_key = true
		}

		if set_key {
			fmt.Printf("%s - %v\n", address, amount)

			err = db.Update(func(txn *badger.Txn) error {
				err := txn.Set([]byte(address), []byte(strconv.FormatFloat(amount, 'f', -1, 64)))
				return err
			})
		}
		// Don't use defer in loop, just discard the tx at the end of each loop
		txn.Discard()
	}

	//err = db.View(func(txn *badger.Txn) error {
	//	opts := badger.DefaultIteratorOptions
	//	opts.PrefetchSize = 10
	//	it := txn.NewIterator(opts)
	//	for it.Rewind(); it.Valid(); it.Next() {
	//		item := it.Item()
	//		k := item.Key()
	//		v, err := item.Value()
	//		if err != nil {
	//			return err
	//		}
	//		fmt.Printf("key=%s, value=%s\n", k, v)
	//	}
	//	return nil
	//})

	// Get xvc_last_tx from db
	lastHash := "7f1d1076d7cf0da5527df59c92f44c0555a4ba7e27e73eb0e3181926a4de9cfd"
	hashFound := false
	txn := db.NewTransaction(false)
	item, err := txn.Get([]byte("last_hash"))
	if err != nil {
		fmt.Printf("hashFound %s\n", err)
	} else {
		hashFound = true
	}
	if hashFound {
		val, err := item.Value()
		if err != nil {
			fmt.Printf("%s\n", err)
		}
		lastHash = string(val)
		fmt.Printf("last_hash %s\n\n", lastHash)
	}
	txn.Discard()

	fmt.Printf("\n* RpcListSinceBlock\n")
	// Lists all transactions since the block with the hash
	listTxs := xvc.RpcListSinceBlock(lastHash)
	//show_data(listTxs)
	result := listTxs["result"].(map[string]interface{})
	lastHash = result["lastblock"].(string)
	transactions := result["transactions"].([]interface{})
	for k, v := range transactions {
		tx := v.(map[string]interface{})
		category := tx["category"].(string)
		if category == "receive" {
			house_address := tx["address"].(string)
			trans_amount := tx["amount"].(float64)
			txid := tx["txid"].(string)
			// Orphan txs do not have blockhash info
			blockhash := tx["blockhash"].(string)
			txdata := xvc.RpcGetTransaction(txid)
			// Let's do some magic!
			user_address := getSenderAddressFromTx(txdata)
			fmt.Printf("%v %s %v %s %s %s %s\n", k, category, trans_amount, txid, house_address, user_address, blockhash)
		} else {
			//Skip orphans and send
			continue
		}
	}
	fmt.Printf("Lastblock %s\n", lastHash)

	//fmt.Printf("\n\n* Get transactions\n")
	//// After the check we will have all the needed data (HouseAddress, UserAddress, bet_amount)
	//response = xvc.RpcListTransactions("*", "90", "0")
	//for _, u := range response["result"].([]interface{}) {
	//	vv := u.(map[string]interface{})
	//	category := vv["category"].(string)
	//	house_address := vv["address"].(string)
	//	trans_amount := vv["amount"].(float64)
	//	txid := vv["txid"].(string)
	//	blockhash := vv["blockhash"].(string)
	//	txdata := xvc.RpcGetTransaction(txid)
	//	// Let's do some magic!
	//	user_address := getSenderAddressFromTx(txdata)
	//	if category == "receive" {
	//		fmt.Printf("%s %v %s %s %s %s\n", category, trans_amount, txid, house_address, user_address, blockhash)
	//	} else {
	//		fmt.Printf("%s %v %s\n", category, trans_amount, blockhash)
	//	}
	//	lastHash = blockhash
	//}

	err = db.Update(func(txn *badger.Txn) error {
		err := txn.Set([]byte("last_hash"), []byte(lastHash))
		return err
	})

	db.PurgeOlderVersions()

	elapsed := time.Since(start)
	fmt.Printf("check_received took %s\n", elapsed)

}

func getSenderAddressFromTx(tx map[string]interface{}) string {
	// Let's do some magic!
	vout := tx["result"].(map[string]interface{})["vout"]
	scriptPubKey := vout.([]interface{})[0].(map[string]interface{})["scriptPubKey"]
	senderAddr := scriptPubKey.(map[string]interface{})["addresses"].([]interface{})[0].(string)
	return senderAddr
}

func show_data(data map[string]interface{}) {
	for k, v := range data {
		switch vv := v.(type) {
		case string:
			fmt.Println(k, "is string", vv)
		case float64:
			fmt.Println(k, "is float64", vv)
		case []interface{}:
			fmt.Println(k, "is an array:")
			for i, u := range vv {
				fmt.Println("List", i, u)
			}
		case map[string]interface{}:
			fmt.Println(k, "is an array:")
			for i, u := range vv {
				fmt.Println("Map", i, u)
			}
		default:
			fmt.Println(k, "is of a type I don't know how to handle")
		}
	}
}
