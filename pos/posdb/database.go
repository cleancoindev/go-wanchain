package posdb

import (
	"crypto/ecdsa"
	"io/ioutil"
	"math/big"
	"os"
	"path"
	"strings"
	"sync"

	"github.com/wanchain/go-wanchain/common"
	"github.com/wanchain/go-wanchain/rlp"

	"github.com/wanchain/go-wanchain/ethdb"
	"github.com/wanchain/go-wanchain/log"
	"github.com/wanchain/go-wanchain/pos/posconfig"
	"github.com/wanchain/go-wanchain/pos/postools"
	bn256 "github.com/wanchain/pos/cloudflare"
)

//Db is the wanpos leveldb class
type Db struct {
	db *ethdb.LDBDatabase
}

var (
	dbInstMap = make(map[string]*Db)
	//RANDOMBEACON_DB_KEY = "PosRandomBeacon"
	selecter SelectLead
	mu       sync.RWMutex
)

func NewDb(fileName string) *Db {
	mu.Lock()
	defer mu.Unlock()
	db := GetDbByName(fileName)
	if db != nil {
		return db
	}

	dbInst := &Db{db: nil}

	dbInst.DbInit(fileName)

	dbInstMap[fileName] = dbInst

	return dbInst
}

var dbInstance *Db

func init() {
	dbInstance = NewDb("")
}

// DbInitAll init all db files
func DbInitAll(pathname string) {
	posconfig.Cfg().Dbpath = pathname
	dbInstance = NewDb(posconfig.PosLocalDB)
	NewDb(posconfig.RbLocalDB)
	NewDb(posconfig.EpLocalDB)
}

//GetDb can get a Db instance to use
func GetDb() *Db {
	return dbInstance
}

func GetDbByName(name string) *Db {
	return dbInstMap[name]
}

//func GetGenesisRandon() *big.Int {
//	return big.NewInt(1)
//}
//
//func GetRandom(epochId uint64) (*big.Int, error) {
//	bt, err := GetDb().Get(epochId, RANDOMBEACON_DB_KEY)
//	if err != nil {
//		if epochId == 0 {
//			return GetGenesisRandon(), nil
//		}
//
//		return nil, err
//	}
//
//	return new(big.Int).SetBytes(bt), nil
//}
//
//func SetRandom(epochId uint64, random *big.Int) error {
//	_, err := GetDb().Put(epochId, RANDOMBEACON_DB_KEY, random.Bytes())
//	return err
//}

//DbInit use to init leveldb in this object, user should not use this. It is automate called in init().
func (s *Db) DbInit(dbPath string) {
	var dirname string
	var err error
	if dbPath == "" {
		dirname, err = ioutil.TempDir(os.TempDir(), "wanpos_tmpdb_")
		if err != nil {
			panic("failed to create wanpos_tmpdb file: " + err.Error())
		}
	} else {
		nameIdx := strings.LastIndex(dbPath, string(os.PathSeparator))
		if nameIdx < 0 {
			dirname = path.Join(posconfig.Cfg().Dbpath, "gwan")
			dirname = path.Join(dirname, dbPath)
		} else {
			dbPath = path.Join(dbPath, "gwan")
			dirname = path.Join(dbPath, "wanposdb")
		}
	}

	if s.db != nil {
		s.db.Close()
	}

	inst, ok := dbInstMap[dbPath]

	if ok {
		inst.DbClose()
	}

	s.db, err = ethdb.NewLDBDatabase(dirname, 0, 0)
	if err != nil {
		panic("failed to create wanpos_tmpdb database: " + dbPath + "_" + err.Error())
	}
}

func (s *Db) put(epochID uint64, index uint64, key string, value []byte, saveKey bool) ([]byte, error) {
	newKey := s.getUniqueKeyBytes(epochID, index, key)

	has, err := s.db.Has(newKey)
	if err != nil {
		return nil, err
	}

	if has {
		saveKey = false
	}

	s.db.Put(newKey, value)
	if saveKey {
		err = s.saveKey(newKey, epochID)
	}
	return newKey, err
}

//PutWithIndex use to set a key-value store with a given epochID and Index
func (s *Db) PutWithIndex(epochID uint64, index uint64, key string, value []byte) ([]byte, error) {
	return s.put(epochID, index, key, value, true)
}

//GetWithIndex use to get a key-value store with a given epochID and Index
func (s *Db) GetWithIndex(epochID uint64, index uint64, key string) ([]byte, error) {
	newKey := s.getUniqueKeyBytes(epochID, index, key)

	ret, err := s.db.Get(newKey)
	if err != nil {
		//debug.PrintStack()
	}
	return ret, err
}

//Put use to set a key-value store with a given epochID
func (s *Db) Put(epochID uint64, key string, value []byte) ([]byte, error) {
	return s.PutWithIndex(epochID, 0, key, value)
}

//Get use to get a key-value store with a given epochID
func (s *Db) Get(epochID uint64, key string) ([]byte, error) {
	return s.GetWithIndex(epochID, 0, key)
}

func (s *Db) saveKey(key []byte, epochID uint64) error {
	keyCount := s.getKeyCount(epochID)

	keyName := s.getKeyName(epochID, keyCount)

	_, err := s.putNoCount(0, keyName, key)
	if err != nil {
		return err
	}

	keyCount++
	_, err = s.putNoCount(0, s.getKeyCountName(epochID), Uint64ToBytes(keyCount))
	if err != nil {
		return err
	}

	return nil
}

func (s *Db) getKeyCount(epochID uint64) uint64 {
	ret, err := s.Get(0, s.getKeyCountName(epochID))
	if err != nil {
		return 0
	}
	return BytesToUint64(ret)
}

func (s *Db) getAllKeys(epochID uint64) []string {
	keyCount := s.getKeyCount(epochID)

	keys := make([]string, keyCount)
	var i uint64
	for i = 0; i < keyCount; i++ {
		keyName := s.getKeyName(epochID, i)
		ret, err := s.Get(0, keyName)
		if err != nil {
			log.Warn(err.Error())
			continue
		}

		keys[i] = string(ret)
	}

	return keys
}

func (s *Db) getKeyName(epochID uint64, keyIndex uint64) string {
	return "key_" + Uint64ToString(epochID) + "_" + Uint64ToString(keyIndex)
}

func (s *Db) getKeyCountName(epochID uint64) string {
	return "keyCount_" + Uint64ToString(epochID)
}

func (s *Db) putNoCount(epochID uint64, key string, value []byte) ([]byte, error) {
	return s.put(epochID, 0, key, value, false)
}

//DbClose use to close db file
func (s *Db) DbClose() {
	s.db.Close()
}

// GetStorageByteArray : cb is callback function. cb return true indicating like to continue, return false indicating stop
func (s *Db) GetStorageByteArray(epochID uint64) [][]byte {
	//searchKey := s.getUniqueKey(epochID, 0, "")
	//
	//searchKey = strings.Split(searchKey, "_")[0]

	keys := s.getAllKeys(epochID)

	arrays := make([][]byte, len(keys))

	for i := 0; i < len(keys); i++ {
		ret, err := s.db.Get([]byte(keys[i]))
		if err != nil {
			log.Warn(err.Error())
			continue
		}
		arrays[i] = ret
	}

	return arrays
}

func (s *Db) getUniqueKey(epochID uint64, index uint64, key string) string {
	uskey := Uint64ToString(epochID) + "_" + Uint64ToString(index) + "_" + key
	return uskey
}

func (s *Db) getUniqueKeyBytes(epochID uint64, index uint64, key string) []byte {
	return []byte(s.getUniqueKey(epochID, index, key))
}

//-------------common functions---------------------------------------

//PkEqual only can use in same curve. return whether the two points equal
func PkEqual(pk1, pk2 *ecdsa.PublicKey) bool {
	return postools.PkEqual(pk1, pk2)
}

// Uint64ToBytes use a big.Int to transfer uint64 to bytes
// Must use big.Int to reverse
func Uint64ToBytes(input uint64) []byte {
	return postools.Uint64ToBytes(input)
}

// BytesToUint64 use a big.Int to transfer uint64 to bytes
// Must input a big.Int bytes
func BytesToUint64(input []byte) uint64 {
	return postools.BytesToUint64(input)
}

// Uint64ToString can change uint64 to string through a big.Int, output is a 10 base number
func Uint64ToString(input uint64) string {
	return postools.Uint64ToString(input)
}

// Uint64StringToByte can change uint64  string to bytes through a big.Int, Input must be a 10 base number
func Uint64StringToByte(input string) []byte {
	return postools.Uint64StringToByte(input)
}

// StringToUint64 can change string to uint64 through a big.Int, Input must be a 10 base number
func StringToUint64(input string) uint64 {
	return postools.StringToUint64(input)
}

// BigIntArrayToByteArray can change []*big.Int to [][]byte
func BigIntArrayToByteArray(input []*big.Int) [][]byte {
	return postools.BigIntArrayToByteArray(input)
}

// ByteArrayToBigIntArray can change [][]byte to big.Int
func ByteArrayToBigIntArray(input [][]byte) []*big.Int {
	return postools.ByteArrayToBigIntArray(input)
}

// PkArrayToByteArray can change []*ecdsa.PublicKey to [][]byte
func PkArrayToByteArray(input []*ecdsa.PublicKey) [][]byte {
	return postools.PkArrayToByteArray(input)
}

// ByteArrayToPkArray can change [][]byte to []*ecdsa.PublicKey
func ByteArrayToPkArray(input [][]byte) []*ecdsa.PublicKey {
	return postools.ByteArrayToPkArray(input)
}

type SelectLead interface {
	SelectLeadersLoop(epochId uint64) error
	GetEpochLeaders(epochID uint64) [][]byte
	GetProposerBn256PK(epochID uint64, idx uint64, addr common.Address) []byte
}

func SetEpocherInst(sor SelectLead) {
	selecter = sor
}

func GetEpocherInst() SelectLead {
	// TODO: can't be nil
	if selecter == nil {
		panic("GetEpocherInst")
	}
	return selecter
}

// TODO duplicated with epochLeader
type Proposer struct {
	PubSec256     []byte
	PubBn256      []byte
	Probabilities *big.Int
}

func GetRBProposerGroup(epochId uint64) []bn256.G1 {
	db := NewDb(posconfig.RbLocalDB)
	if db == nil {
		log.Error("GetRBProposerGroup create db error")
		return nil
	}

	proposersArray := db.GetStorageByteArray(epochId)
	length := len(proposersArray)
	g1s := make([]bn256.G1, length, length)

	for i := 0; i < length; i++ {
		proposer := Proposer{}
		err := rlp.DecodeBytes(proposersArray[i], &proposer)
		if err != nil {
			log.Error("can't rlp decode:", err)
		}
		g1s[i] = *new(bn256.G1)
		g1s[i].Unmarshal(proposer.PubBn256)
	}

	return g1s

}
func GetEpochLeaderGroup(epochId uint64) [][]byte {
	db := NewDb(posconfig.EpLocalDB)
	if db == nil {
		log.Error("GetEpochLeaderGroup create db error")
		return nil
	}

	proposersArray := db.GetStorageByteArray(epochId)
	length := len(proposersArray)
	pks := make([][]byte, length, length)

	for i := 0; i < length; i++ {
		proposer := Proposer{}
		err := rlp.DecodeBytes(proposersArray[i], &proposer)
		if err != nil {
			log.Error("can't rlp decode:", err)
		}
		pks[i] = proposer.PubSec256
	}

	return pks

}

func GetProposerBn256PK(epochID uint64, idx uint64, addr common.Address) []byte {
	return selecter.GetProposerBn256PK(epochID, idx, addr)
}

//-------------------------------------------------------------------

var (
	lastBlockEpoch = make(map[uint64]uint64)
)

var lastEpochId = uint64(0)

func UpdateEpochBlock(epochID uint64, blockNumber uint64) {
	if epochID != lastEpochId {
		if epochID > lastEpochId {
			GetEpocherInst().SelectLeadersLoop(epochID + 1)
		}
		lastEpochId = epochID
	}
	lastBlockEpoch[epochID] = blockNumber
}
func SetEpochBlock(epochID uint64, blockNumber uint64) {
	lastBlockEpoch[epochID] = blockNumber
}
func GetEpochBlock(epochID uint64) uint64 {
	return lastBlockEpoch[epochID]
}
