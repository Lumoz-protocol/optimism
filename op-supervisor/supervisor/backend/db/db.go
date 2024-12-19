package db

import (
	"errors"
	"fmt"
	"io"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"

	"github.com/ethereum-optimism/optimism/op-service/eth"
	"github.com/ethereum-optimism/optimism/op-service/locks"
	"github.com/ethereum-optimism/optimism/op-supervisor/supervisor/backend/db/fromda"
	"github.com/ethereum-optimism/optimism/op-supervisor/supervisor/backend/db/logs"
	"github.com/ethereum-optimism/optimism/op-supervisor/supervisor/backend/depset"
	"github.com/ethereum-optimism/optimism/op-supervisor/supervisor/types"
	gethevent "github.com/ethereum/go-ethereum/event"
)

type LogStorage interface {
	io.Closer

	AddLog(logHash common.Hash, parentBlock eth.BlockID,
		logIdx uint32, execMsg *types.ExecutingMessage) error

	SealBlock(parentHash common.Hash, block eth.BlockID, timestamp uint64) error

	Rewind(newHeadBlockNum uint64) error

	LatestSealedBlockNum() (n uint64, ok bool)

	// FindSealedBlock finds the requested block by number, to check if it exists,
	// returning the block seal if it was found.
	// returns ErrFuture if the block is too new to be able to tell.
	FindSealedBlock(number uint64) (block types.BlockSeal, err error)

	IteratorStartingAt(sealedNum uint64, logsSince uint32) (logs.Iterator, error)

	// Contains returns no error iff the specified logHash is recorded in the specified blockNum and logIdx.
	// If the log is out of reach, then ErrFuture is returned.
	// If the log is determined to conflict with the canonical chain, then ErrConflict is returned.
	// logIdx is the index of the log in the array of all logs in the block.
	// This can be used to check the validity of cross-chain interop events.
	// The block-seal of the blockNum block, that the log was included in, is returned.
	// This seal may be fully zeroed, without error, if the block isn't fully known yet.
	Contains(blockNum uint64, logIdx uint32, logHash common.Hash) (includedIn types.BlockSeal, err error)

	// OpenBlock accumulates the ExecutingMessage events for a block and returns them
	OpenBlock(blockNum uint64) (ref eth.BlockRef, logCount uint32, execMsgs map[uint32]*types.ExecutingMessage, err error)
}

type LocalDerivedFromStorage interface {
	First() (derivedFrom types.BlockSeal, derived types.BlockSeal, err error)
	Latest() (derivedFrom types.BlockSeal, derived types.BlockSeal, err error)
	AddDerived(derivedFrom eth.BlockRef, derived eth.BlockRef) error
	LastDerivedAt(derivedFrom eth.BlockID) (derived types.BlockSeal, err error)
	DerivedFrom(derived eth.BlockID) (derivedFrom types.BlockSeal, err error)
	FirstAfter(derivedFrom, derived eth.BlockID) (nextDerivedFrom, nextDerived types.BlockSeal, err error)
	NextDerivedFrom(derivedFrom eth.BlockID) (nextDerivedFrom types.BlockSeal, err error)
	NextDerived(derived eth.BlockID) (derivedFrom types.BlockSeal, nextDerived types.BlockSeal, err error)
	PreviousDerivedFrom(derivedFrom eth.BlockID) (prevDerivedFrom types.BlockSeal, err error)
	PreviousDerived(derived eth.BlockID) (prevDerived types.BlockSeal, err error)
}

var _ LocalDerivedFromStorage = (*fromda.DB)(nil)

type CrossDerivedFromStorage interface {
	LocalDerivedFromStorage
	// This will start to differ with reorg support
}

var _ LogStorage = (*logs.DB)(nil)

// ChainsDB is a database that stores logs and derived-from data for multiple chains.
// it implements the LogStorage interface, as well as several DB interfaces needed by the cross package.
type ChainsDB struct {
	// unsafe info: the sequence of block seals and events
	logDBs locks.RWMap[types.ChainID, LogStorage]

	// cross-unsafe: how far we have processed the unsafe data.
	// If present but set to a zeroed value the cross-unsafe will fallback to cross-safe.
	crossUnsafe locks.RWMap[types.ChainID, *locks.RWValue[types.BlockSeal]]

	// local-safe: index of what we optimistically know about L2 blocks being derived from L1
	localDBs locks.RWMap[types.ChainID, LocalDerivedFromStorage]

	// cross-safe: index of L2 blocks we know to only have cross-L2 valid dependencies
	crossDBs locks.RWMap[types.ChainID, CrossDerivedFromStorage]

	// gethevent Feeds allow for subscription to events in the DB
	l2FinalitySubscription locks.RWMap[types.ChainID, *gethevent.FeedOf[eth.BlockID]]
	crossSafeSubscription  locks.RWMap[types.ChainID, *gethevent.FeedOf[types.DerivedPair]]

	// finalized: the L1 finality progress. This can be translated into what may be considered as finalized in L2.
	// It is initially zeroed, and the L2 finality query will return
	// an error until it has this L1 finality to work with.
	finalizedL1 locks.RWValue[eth.L1BlockRef]

	// depSet is the dependency set, used to determine what may be tracked,
	// what is missing, and to provide it to DB users.
	depSet depset.DependencySet

	logger log.Logger
}

func NewChainsDB(l log.Logger, depSet depset.DependencySet) *ChainsDB {
	return &ChainsDB{
		logger: l,
		depSet: depSet,
	}
}

func (db *ChainsDB) AddLogDB(chainID types.ChainID, logDB LogStorage) {
	if db.logDBs.Has(chainID) {
		db.logger.Warn("overwriting existing log DB for chain", "chain", chainID)
	}

	db.logDBs.Set(chainID, logDB)
}

func (db *ChainsDB) AddLocalDerivedFromDB(chainID types.ChainID, dfDB LocalDerivedFromStorage) {
	if db.localDBs.Has(chainID) {
		db.logger.Warn("overwriting existing local derived-from DB for chain", "chain", chainID)
	}

	db.localDBs.Set(chainID, dfDB)
}

func (db *ChainsDB) AddCrossDerivedFromDB(chainID types.ChainID, dfDB CrossDerivedFromStorage) {
	if db.crossDBs.Has(chainID) {
		db.logger.Warn("overwriting existing cross derived-from DB for chain", "chain", chainID)
	}

	db.crossDBs.Set(chainID, dfDB)
}

func (db *ChainsDB) AddCrossUnsafeTracker(chainID types.ChainID) {
	if db.crossUnsafe.Has(chainID) {
		db.logger.Warn("overwriting existing cross-unsafe tracker for chain", "chain", chainID)
	}
	db.crossUnsafe.Set(chainID, &locks.RWValue[types.BlockSeal]{})
}

func (db *ChainsDB) AddSubscriptions(chainID types.ChainID) {
	if db.l2FinalitySubscription.Has(chainID) {
		db.logger.Warn("a subscription for this chain already exists", "chain", chainID)
		return
	}
	if db.crossSafeSubscription.Has(chainID) {
		db.logger.Warn("a subscription for this chain already exists", "chain", chainID)
		return
	}
	db.l2FinalitySubscription.Set(chainID, &gethevent.FeedOf[eth.BlockID]{})
	db.crossSafeSubscription.Set(chainID, &gethevent.FeedOf[types.DerivedPair]{})
}

func (db *ChainsDB) SubscribeCrossSafe(chainID types.ChainID, c chan<- types.DerivedPair) (gethevent.Subscription, error) {
	sub, ok := db.crossSafeSubscription.Get(chainID)
	if !ok {
		return nil, fmt.Errorf("cannot subscribe to cross-safe: %w: %s", types.ErrUnknownChain, chainID)
	}
	return sub.Subscribe(c), nil
}

func (db *ChainsDB) SubscribeFinalized(chainID types.ChainID, c chan<- eth.BlockID) (gethevent.Subscription, error) {
	sub, ok := db.l2FinalitySubscription.Get(chainID)
	if !ok {
		return nil, fmt.Errorf("cannot subscribe to cross-safe: %w: %s", types.ErrUnknownChain, chainID)
	}
	return sub.Subscribe(c), nil
}

// ResumeFromLastSealedBlock prepares the chains db to resume recording events after a restart.
// It rewinds the database to the last block that is guaranteed to have been fully recorded to the database,
// to ensure it can resume recording from the first log of the next block.
func (db *ChainsDB) ResumeFromLastSealedBlock() error {
	var result error
	db.logDBs.Range(func(chain types.ChainID, logStore LogStorage) bool {
		headNum, ok := logStore.LatestSealedBlockNum()
		if !ok {
			// db must be empty, nothing to rewind to
			db.logger.Info("Resuming, but found no DB contents", "chain", chain)
			return true
		}
		db.logger.Info("Resuming, starting from last sealed block", "head", headNum)
		if err := logStore.Rewind(headNum); err != nil {
			result = fmt.Errorf("failed to rewind chain %s to sealed block %d", chain, headNum)
			return false
		}
		return true
	})
	return result
}

func (db *ChainsDB) DependencySet() depset.DependencySet {
	return db.depSet
}

func (db *ChainsDB) Close() error {
	var combined error
	db.logDBs.Range(func(id types.ChainID, logDB LogStorage) bool {
		if err := logDB.Close(); err != nil {
			combined = errors.Join(combined, fmt.Errorf("failed to close log db for chain %v: %w", id, err))
		}
		return true
	})
	return combined
}
