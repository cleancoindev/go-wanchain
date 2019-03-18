package epochLeader

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strings"

	"github.com/wanchain/go-wanchain/rlp"

	"github.com/wanchain/go-wanchain/common"
	"github.com/wanchain/go-wanchain/core"
	"github.com/wanchain/go-wanchain/core/state"
	"github.com/wanchain/go-wanchain/core/vm"
	"github.com/wanchain/go-wanchain/crypto"
	"github.com/wanchain/go-wanchain/log"
	"github.com/wanchain/go-wanchain/params"
	"github.com/wanchain/go-wanchain/pos/posconfig"
	"github.com/wanchain/go-wanchain/pos/posdb"
)

var (
	safeK = uint64(1)
	Nr    = posconfig.RandomProperCount //num of random proposers
	Ne    = posconfig.EpochLeaderCount  //num of epoch leaders, limited <= 256 now

	Big1                                   = big.NewInt(1)
	Big0                                   = big.NewInt(0)
	ErrInvalidRandomProposerSelection      = errors.New("Invalid Random Proposer Selection")                  //Invalid Random Proposer Selection
	ErrInvalidEpochProposerSelection       = errors.New("Invalid Epoch Proposer Selection")                   //Invalid Random Proposer Selection
	ErrInvalidProbabilityfloat2big         = errors.New("Invalid Transform Probability From Float To Bigint") //Invalid Transform Probability From Float To Bigint
	ErrInvalidGenerateCommitment           = errors.New("Invalid Commitment Generation")                      //Invalid Commitment Generation
	ErrInvalidArrayPieceGeneration         = errors.New("Invalid ArrayPiece Generation")                      //Invalid ArrayPiece Generation
	ErrInvalidDleqProofGeneration          = errors.New("Invalid DLEQ Proof Generation")                      //Invalid DLEQ Proof Generation
	ErrInvalidSecretMessageArrayGeneration = errors.New("Invalid Secret Message Array Generation")            //Invalid Secret Message Array Generation
	ErrInvalidSortPublicKeys               = errors.New("Invalid PublicKeys Sort Operation")                  //Invalid PublicKeys Sort Operation
	ErrInvalidSlotLeaderSequenceGeneration = errors.New("Invalid Slot Leader Sequence Generation")            //Invalid Slot Leader Sequence Generation
	ErrInvalidSlotLeaderLocation           = errors.New("Invalid Slot Leader Location")                       //Invalid Slot Leader Location
	ErrInvalidSlotLeaderProofGeneration    = errors.New("Invalid Slot Leader Proof Generation")               //Invalid Slot Leader Proof Generation
)

type Epocher struct {
	rbLeadersDb     *posdb.Db
	rbLeadersAddrDb *posdb.Db
	epochLeadersDb  *posdb.Db
	blkChain        *core.BlockChain
}

var epocherInst *Epocher = nil

type puksInfo struct {
	PubSec256 []byte //staker’s ethereum public key
	PubBn256  []byte //staker’s bn256 public key
}

func NewEpocher(blc *core.BlockChain) *Epocher {

	if blc == nil {
		return nil
	}

	if epocherInst == nil {
		epocherInst = NewEpocherWithLBN(blc, posconfig.RbLocalDB, posconfig.EpLocalDB)
	}

	return epocherInst
}

func GetEpocher() *Epocher {
	return epocherInst
}

func NewEpocherWithLBN(blc *core.BlockChain, rbn string, epdbn string) *Epocher {

	rbdb := posdb.NewDb(rbn)
	rbldAddrDb := posdb.NewDb(rbn + "address")

	epdb := posdb.NewDb(epdbn)
	inst := &Epocher{rbdb, rbldAddrDb, epdb, blc}

	posdb.SetEpocherInst(inst)

	return inst
}

func (e *Epocher) GetBlkChain() *core.BlockChain {
	return e.blkChain
}

func (e *Epocher) GetTargetBlkNumber(epochId uint64) uint64 {
	// TODO how to get thee target blockNumber
	if epochId < 2 {
		return uint64(0)
	}
	targetEpochId := epochId - 2
	targetBlkNum := posdb.GetEpochBlock(targetEpochId)
	if targetBlkNum == 0 {
		curNum := e.blkChain.CurrentBlock().NumberU64()
		for {
			curBlock := e.blkChain.GetBlockByNumber(curNum)
			curEpochId := curBlock.Header().Difficulty.Uint64() >> 32
			if curEpochId <= targetEpochId {
				break
			}
			curNum--
		}
		targetBlkNum = curNum
		posdb.SetEpochBlock(targetEpochId, targetBlkNum)
	}
	return targetBlkNum

}
func (e *Epocher) SelectLeadersLoop(epochId uint64) error {

	targetBlkNum := e.GetTargetBlkNumber(epochId)

	stateDb, err := e.blkChain.StateAt(e.blkChain.GetBlockByNumber(targetBlkNum).Root())
	if err != nil {
		return err
	}

	epochIdIn := epochId
	if epochIdIn > 0 {
		epochIdIn--
	}
	rb := vm.GetR(stateDb, epochIdIn)

	if rb == nil {
		log.Error(fmt.Sprintln("vm.GetR return nil at epochId:", epochId))
		rb = big.NewInt(1)
	}

	r := rb.Bytes()

	err = e.SelectLeaders(r, Ne, Nr, stateDb, epochId)
	if err != nil {
		return err
	}

	return nil
}

func (e *Epocher) SelectLeaders(r []byte, ne int, nr int, statedb *state.StateDB, epochId uint64) error {

	log.Debug("select randoms", epochId, common.ToHex(r))

	pa, err := e.createStakerProbabilityArray(statedb, epochId)
	if pa == nil || err != nil {
		return err
	}

	e.epochLeaderSelection(r, ne, pa, epochId)

	e.randomProposerSelection(r, nr, pa, epochId)

	return nil

}

type Proposer struct {
	PubSec256     []byte
	PubBn256      []byte
	Probabilities *big.Int
}

type ProposerSorter []Proposer

func newProposerSorter() ProposerSorter {
	ps := make(ProposerSorter, 0)
	return ps
}

//Len()
func (s ProposerSorter) Len() int {
	return len(s)
}

func (s ProposerSorter) Less(i, j int) bool {
	return s[i].Probabilities.Cmp(s[j].Probabilities) < 0
}

//Swap()
func (s ProposerSorter) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func Round(f float64, n int) float64 {
	n10 := math.Pow10(n)
	return math.Trunc((f+0.5/n10)*n10) / n10
}

/*
新的wancoin到stake的权重计算公式
Probability = Amount * (10 + lockEpoch/(maxEpoch/10)) * (2-exp(t-1))
其中, Amount是注册进来的wan数量
lockEpoch是要锁定的时长(epoch数), maxEpoch是运行锁定的最长时长, 目前是1000
这样, 锁定时长对应的权重在[10,20]之间.
t是锁定时间剩余时长的百分比,取值(1,0)之间, (2-exp(t-1))对应的权重在(1, 1.64)之间.
*/
const Accuracy float64 = 1024.0 //accuracy to magnificate
func (e *Epocher) calProbability(epochId uint64, amountWin *big.Int, lockTime uint64, startEpochId uint64) *big.Int {
	amount := big.NewInt(0).Div(amountWin, big.NewInt(params.Wan))
	pb := big.NewInt(0)
	var leftTimePercent float64
	if epochId < 2 {
		leftTimePercent = 1
	} else if epochId < startEpochId+2 || epochId >= startEpochId+2+lockTime {
		leftTimePercent = 0
	} else {
		leftTimePercent = (float64(lockTime-(epochId-startEpochId-2)) / float64(lockTime))
	}
	fpercent := 2 - Round(math.Exp(leftTimePercent-1), 4)

	epercent := big.NewInt(int64(fpercent * Accuracy))

	lockWeight := vm.CalLocktimeWeight(lockTime)
	timeBig := big.NewInt(int64(lockWeight))

	pb.Mul(amount, epercent)
	pb.Mul(pb, timeBig)

	log.Debug("calProbability,pb:", pb)

	return pb
}

//wanhumber*locktime*(exp-(t) ),t=(locktime - passedtime/locktime)
func (e *Epocher) generateProblility(pstaker *vm.StakerInfo, epochId uint64) (*Proposer, error) {

	pb := e.calProbability(epochId, pstaker.Amount, pstaker.LockEpochs, pstaker.StakingEpoch)
	for i := 0; i < len(pstaker.Clients); i++ {
		lockEpoch := pstaker.LockEpochs - (pstaker.Clients[i].StakingEpoch - pstaker.StakingEpoch)
		pb.Add(pb, e.calProbability(epochId, pstaker.Clients[i].Amount, lockEpoch, pstaker.Clients[i].StakingEpoch))
	}
	p := &Proposer{
		PubSec256:     pstaker.PubSec256,
		PubBn256:      pstaker.PubBn256,
		Probabilities: pb,
	}

	return p, nil

}

func (e *Epocher) createStakerProbabilityArray(statedb *state.StateDB, epochId uint64) (ProposerSorter, error) {

	if statedb == nil {
		return nil, vm.ErrUnknown
	}

	listAddr := vm.StakersInfoAddr
	ps := newProposerSorter()

	//blkTime := epochId*(posconfig.SlotTime*posconfig.SlotCount) + posconfig.EpochBaseTime

	statedb.ForEachStorageByteArray(listAddr, func(key common.Hash, value []byte) bool {

		staker := vm.StakerInfo{}
		err := json.Unmarshal(value, &staker)
		if err != nil {
			log.Info(err.Error())
			return true
		}

		if staker.Amount.Cmp(Big0) == 0 {
			//log.Info("staker ",common.ToHex(staker.PubSec256),"stake out already")
			return true
		}

		pitem, err := e.generateProblility(&staker, epochId)
		if err != nil {
			log.Info(err.Error())
			return true
		}

		if staker.Amount.Cmp(Big0) > 0 && (*pitem).Probabilities.Cmp(Big0) > 0 {
			ps = append(ps, *pitem)
			log.Info(common.ToHex((*pitem).Probabilities.Bytes()))
		}

		return true
	})

	sort.Sort(ProposerSorter(ps))

	for idx, _ := range ps {
		if idx == 0 {
			continue
		}

		ps[idx].Probabilities = big.NewInt(0).Add(ps[idx].Probabilities, ps[idx-1].Probabilities)
	}

	log.Debug("get createStakerProbabilityArray len=", len(ps))

	return ps, nil
}

//samples nr random proposers by random number r（Random Beacon) from PublicKeys based on proportion of Probabilities
func (e *Epocher) epochLeaderSelection(r []byte, nr int, ps ProposerSorter, epochId uint64) error {
	if r == nil || nr <= 0 || len(ps) == 0 {
		return ErrInvalidRandomProposerSelection
	}

	//the last one is total properties
	tp := ps[len(ps)-1].Probabilities

	var Byte0 = []byte{byte(0)}
	var buffer bytes.Buffer
	buffer.Write(Byte0)
	buffer.Write(r)
	r0 := buffer.Bytes()       //r0 = 0||r
	cr := crypto.Keccak256(r0) //cr = hash(r0)

	//randomProposerPublicKeys := make([]*ecdsa.PublicKey, 0)  //store the selected publickeys
	log.Info("epochLeaderSelection selecting")
	for i := 0; i < nr; i++ {

		crBig := new(big.Int).SetBytes(cr)
		crBig = crBig.Mod(crBig, tp) //cr_big = cr mod tp

		//fmt.Println("epoch leader mod tp=" + common.ToHex(crBig.Bytes()))
		//select pki whose probability bigger than cr_big left
		idx := sort.Search(len(ps), func(i int) bool { return ps[i].Probabilities.Cmp(crBig) > 0 })

		log.Info("select epoch leader", "epochid=", epochId, "idx=", i, "pub=", ps[idx].PubSec256)
		//randomProposerPublicKeys = append(randomProposerPublicKeys, ps[idx].PubSec256)
		val, err := rlp.EncodeToBytes(&ps[idx])
		//val,err := json.Marshal(&ps[idx])
		if err != nil {
			continue
		}
		e.epochLeadersDb.PutWithIndex(epochId, uint64(i), "", val)

		cr = crypto.Keccak256(cr)
	}

	return nil
}

//*bn256.G1
//samples ne epoch leaders by random number r from PublicKeys based on proportion of Probabilities
func (e *Epocher) randomProposerSelection(r []byte, nr int, ps ProposerSorter, epochId uint64) error {
	if r == nil || nr <= 0 || len(ps) == 0 {
		return ErrInvalidEpochProposerSelection
	}

	//the last one is total properties
	tp := ps[len(ps)-1].Probabilities

	var Byte1 = []byte{byte(1)}
	var buffer bytes.Buffer
	buffer.Write(Byte1)
	buffer.Write(r)
	r1 := buffer.Bytes()       //r1 = 1||r
	cr := crypto.Keccak256(r1) //cr = hash(r1)

	log.Info("random proposer selecting...\n")
	for i := 0; i < nr; i++ {

		crBig := new(big.Int).SetBytes(cr)
		crBig = crBig.Mod(crBig, tp) //cr_big = cr mod tp

		//select pki whose probability bigger than cr_big left
		idx := sort.Search(len(ps), func(i int) bool { return ps[i].Probabilities.Cmp(crBig) > 0 })

		val, err := rlp.EncodeToBytes(ps[idx])
		//val,err := json.Marshal(&ps[idx])

		if err != nil {
			continue
		}

		e.rbLeadersDb.PutWithIndex(epochId, uint64(i), "", val)

		cr = crypto.Keccak256(cr)
	}

	return nil
}

//get epochLeaders of epochID in localdb
func (e *Epocher) GetEpochLeaders(epochID uint64) [][]byte {

	// TODO: how to cache these
	ksarray := posdb.GetEpochLeaderGroup(epochID)

	return ksarray

}

//get rbLeaders of epochID in localdb only for incentive.
func (e *Epocher) GetRBProposerGroup(epochID uint64) []vm.Leader {
	proposersArray := e.rbLeadersDb.GetStorageByteArray(epochID)
	length := len(proposersArray)
	g1s := make([]vm.Leader, length)

	for i := 0; i < length; i++ {
		proposer := Proposer{}
		err := rlp.DecodeBytes(proposersArray[i], &proposer)
		if err != nil {
			log.Error("can't rlp decode:", err)
		}
		g1s[i].PubSec256 = proposer.PubSec256
		g1s[i].PubBn256 = proposer.PubBn256
		pub := crypto.ToECDSAPub(proposer.PubSec256)
		if nil == pub {
			continue
		}

		g1s[i].SecAddr = crypto.PubkeyToAddress(*pub)
		g1s[i].Probabilities = proposer.Probabilities
	}

	return g1s
}

func (e *Epocher) GetProposerBn256PK(epochID uint64, idx uint64, addr common.Address) []byte {
	valSet := e.rbLeadersDb.GetStorageByteArray(epochID)

	if valSet == nil || len(valSet) == 0 {
		return nil
	}

	psValue := valSet[idx]

	proposer := Proposer{}
	err := rlp.DecodeBytes(psValue, &proposer)
	if err != nil {
		log.Error("can't rlp decode:", err)
	}

	pub := crypto.ToECDSAPub(proposer.PubSec256)

	if pub == nil {
		return nil
	}

	bingoAddr := crypto.PubkeyToAddress(*pub)

	if bingoAddr == addr {
		return proposer.PubBn256
	} else {
		return nil
	}
}

func (e *Epocher) GetEpochStakers(epochId uint64, puk string) ([]string, error) {

	targetBlkNum := e.GetTargetBlkNumber(epochId)

	stateDb, err := e.blkChain.StateAt(e.blkChain.GetBlockByNumber(targetBlkNum).Root())
	if err != nil {
		return nil, err
	}

	sec256 := common.FromHex(strings.ToLower(puk))
	pubHash := common.BytesToHash(sec256)

	infoArray, err := vm.GetInfo(stateDb, vm.StakersInfoAddr, pubHash)
	if infoArray == nil {
		return nil, errors.New("not find staker staking info")
	}

	var staker vm.StakerInfo
	err = json.Unmarshal(infoArray, &staker)
	if err != nil {
		return nil, err
	}

	if staker.PubSec256 == nil {
		return nil, errors.New("staker has unregistered already")
	}
	//blkTime := epochId*(posconfig.SlotTime*posconfig.SlotCount) + posconfig.EpochBaseTime
	pitem, err := e.generateProblility(&staker, epochId)
	if err != nil {
		return nil, err
	}
	strArray := make([]string, 0)
	val := staker.Amount.Div(staker.Amount, big.NewInt(int64(params.Wan)))

	strArray = append(strArray, fmt.Sprint("amount:", val.Text(10)))
	strArray = append(strArray, fmt.Sprint("lockTime:", staker.LockEpochs))
	strArray = append(strArray, fmt.Sprint("probability:", pitem.Probabilities.Text(10)))

	return strArray, nil

}

func (e *Epocher) GetEpochProbability(epochId uint64, addr common.Address) (infors []vm.ClientProbability, feeRate uint64, totalProbability *big.Int, err error) {

	targetBlkNum := e.GetTargetBlkNumber(epochId)

	stateDb, err := e.blkChain.StateAt(e.blkChain.GetBlockByNumber(targetBlkNum).Root())
	if err != nil {
		return nil, 0, nil, err
	}

	addrHash := common.BytesToHash(addr[:])
	stakerBytes := stateDb.GetStateByteArray(vm.StakersInfoAddr, addrHash)

	staker := vm.StakerInfo{}
	err = json.Unmarshal(stakerBytes, &staker)
	if nil != err {
		return nil, 0, nil, err
	}
	infors = make([]vm.ClientProbability, 1) // TODO: the array length?
	infors[0].Addr = addr
	infors[0].Probability = e.calProbability(epochId, staker.Amount, staker.LockEpochs, staker.StakingEpoch)
	feeRate = staker.FeeRate
	totalProbability = infors[0].Probability //TODO: add all client
	return infors, feeRate, totalProbability, nil
}

func (e *Epocher) SetEpochIncentive(epochId uint64, infors [][]vm.ClientIncentive) (err error) {
	return nil
}

func StakeOutRun(stateDb *state.StateDB, epochID uint64) bool {
	if vm.StakeoutIsFinished(stateDb, epochID) {
		return true
	}
	vm.StakeoutSetEpoch(stateDb, epochID)

	stakers := vm.GetStakersSnap(stateDb)
	for i := 0; i < len(stakers); i++ {
		// stakeout delegated client. client will expire at the same time with delegate node
		staker := stakers[i]
		if epochID < staker.StakingEpoch+staker.LockEpochs+3 { // TODO: check with design
			continue
		}
		for j := 0; j < len(staker.Clients); j++ {
			core.Transfer(stateDb, vm.StakersInfoAddr, staker.Clients[j].Address, staker.Clients[j].Amount)
		}

		core.Transfer(stateDb, vm.StakersInfoAddr, staker.From, staker.Amount)

		vm.UpdateInfo(stateDb, vm.StakersInfoAddr, vm.GetStakeInKeyHash(staker.Address), nil)
	}
	return true
}
