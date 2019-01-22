// Copyright 2018 The dexon-consensus Authors
// This file is part of the dexon-consensus library.
//
// The dexon-consensus library is free software: you can redistribute it
// and/or modify it under the terms of the GNU Lesser General Public License as
// published by the Free Software Foundation, either version 3 of the License,
// or (at your option) any later version.
//
// The dexon-consensus library is distributed in the hope that it will be
// useful, but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU Lesser
// General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the dexon-consensus library. If not, see
// <http://www.gnu.org/licenses/>.

package core

import (
	"testing"
	"time"

	"github.com/dexon-foundation/dexon-consensus/common"
	agrPkg "github.com/dexon-foundation/dexon-consensus/core/agreement"
	"github.com/dexon-foundation/dexon-consensus/core/crypto/ecdsa"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	"github.com/dexon-foundation/dexon-consensus/core/utils"
	"github.com/stretchr/testify/suite"
)

// agreementTestReceiver implements core.agreementReceiver.
type agreementTestReceiver struct {
	s              *AgreementTestSuite
	agreementIndex int
}

func (r *agreementTestReceiver) ProposeVote(vote *types.Vote) {
	r.s.voteChan <- vote
}

func (r *agreementTestReceiver) ProposeBlock() common.Hash {
	block := r.s.proposeBlock(
		r.s.agreement[r.agreementIndex].data.ID,
		r.s.agreement[r.agreementIndex].data.leader.hashCRS)
	r.s.blockChan <- block.Hash
	return block.Hash
}

func (r *agreementTestReceiver) ConfirmBlock(block common.Hash,
	_ []types.Vote) {
	r.s.confirmChan <- block
}

func (r *agreementTestReceiver) PullBlocks(hashes common.Hashes) {
	for _, hash := range hashes {
		r.s.pulledBlocks[hash] = struct{}{}
	}

}

// agreementTestForkReporter implement core.forkReporter.
type agreementTestForkReporter struct {
	s *AgreementTestSuite
}

func (r *agreementTestReceiver) ReportForkVote(v1, v2 *types.Vote) {
	r.s.forkVoteChan <- v1.BlockHash
	r.s.forkVoteChan <- v2.BlockHash
}

func (r *agreementTestReceiver) ReportForkBlock(b1, b2 *types.Block) {
	r.s.forkBlockChan <- b1.Hash
	r.s.forkBlockChan <- b2.Hash
}

func (s *AgreementTestSuite) proposeBlock(
	nID types.NodeID, crs common.Hash) *types.Block {
	block := &types.Block{
		ProposerID: nID,
		Hash:       common.NewRandomHash(),
	}
	s.block[block.Hash] = block
	signer, exist := s.signers[block.ProposerID]
	s.Require().True(exist)
	s.Require().NoError(signer.SignCRS(
		block, crs))
	return block
}

type AgreementTestSuite struct {
	suite.Suite
	ID            types.NodeID
	signers       map[types.NodeID]*utils.Signer
	voteChan      chan *types.Vote
	blockChan     chan common.Hash
	confirmChan   chan common.Hash
	forkVoteChan  chan common.Hash
	forkBlockChan chan common.Hash
	block         map[common.Hash]*types.Block
	pulledBlocks  map[common.Hash]struct{}
	agreement     []*agreement
}

func (s *AgreementTestSuite) SetupTest() {
	prvKey, err := ecdsa.NewPrivateKey()
	s.Require().NoError(err)
	s.ID = types.NewNodeID(prvKey.PublicKey())
	s.signers = map[types.NodeID]*utils.Signer{
		s.ID: utils.NewSigner(prvKey),
	}
	s.voteChan = make(chan *types.Vote, 100)
	s.blockChan = make(chan common.Hash, 100)
	s.confirmChan = make(chan common.Hash, 100)
	s.forkVoteChan = make(chan common.Hash, 100)
	s.forkBlockChan = make(chan common.Hash, 100)
	s.block = make(map[common.Hash]*types.Block)
	s.pulledBlocks = make(map[common.Hash]struct{})
}

func (s *AgreementTestSuite) newAgreement(
	numNotarySet, leaderIdx int, validLeader validLeaderFn) (*agreement, types.NodeID) {
	s.Require().True(leaderIdx < numNotarySet)
	logger := &common.NullLogger{}
	leader := newLeaderSelector(validLeader, logger)
	agreementIdx := len(s.agreement)
	var leaderNode types.NodeID
	for i := 0; i < numNotarySet-1; i++ {
		prvKey, err := ecdsa.NewPrivateKey()
		s.Require().NoError(err)
		nID := types.NewNodeID(prvKey.PublicKey())
		s.signers[nID] = utils.NewSigner(prvKey)
		if i == leaderIdx-1 {
			leaderNode = nID
		}
	}
	if leaderIdx == 0 {
		leaderNode = s.ID
	}
	agreement := newAgreement(
		s.ID,
		&agreementTestReceiver{
			s:              s,
			agreementIndex: agreementIdx,
		},
		leader,
		s.signers[s.ID],
		logger,
	)
	agreement.restart(types.Position{}, leaderNode, common.NewRandomHash())
	s.agreement = append(s.agreement, agreement)
	return agreement, leaderNode
}

func (s *AgreementTestSuite) prepareSignalByCopyingVote(
	signalType agrPkg.SignalType, vote *types.Vote) *agrPkg.Signal {
	votes := make([]types.Vote, 0, len(s.signers))
	for nID := range s.signers {
		v := vote.Clone()
		s.Require().NoError(s.signers[nID].SignVote(v))
		votes = append(votes, *v)
	}
	return agrPkg.NewSignal(signalType, votes)
}

func (s *AgreementTestSuite) prepareVote(nID types.NodeID,
	voteType types.VoteType, hash common.Hash, period uint64) *types.Vote {
	vote := types.NewVote(voteType, hash, period)
	s.Require().NoError(s.signers[nID].SignVote(vote))
	return vote
}

func (s *AgreementTestSuite) prepareSignal(signalType agrPkg.SignalType,
	voteType types.VoteType, hash common.Hash, period uint64) *agrPkg.Signal {
	cnt := 0
	requiredVotes := len(s.signers)/3*2 + 1
	votes := make([]types.Vote, 0, requiredVotes)
	for nID := range s.signers {
		votes = append(votes, *s.prepareVote(nID, voteType, hash, period))
		if cnt++; cnt == requiredVotes {
			break
		}
	}
	return agrPkg.NewSignal(signalType, votes)
}

func (s *AgreementTestSuite) TestSimpleConfirm() {
	a, _ := s.newAgreement(4, -1, func(*types.Block) (bool, error) {
		return true, nil
	})
	// FastState
	a.nextState()
	// FastVoteState
	a.nextState()
	// InitialState
	a.nextState()
	// PreCommitState
	s.Require().Len(s.blockChan, 1)
	blockHash := <-s.blockChan
	block, exist := s.block[blockHash]
	s.Require().True(exist)
	s.Require().NoError(a.processBlock(block))
	s.Require().Len(s.voteChan, 1)
	vote := <-s.voteChan
	s.Equal(types.VoteInit, vote.Type)
	s.Equal(blockHash, vote.BlockHash)
	a.nextState()
	// CommitState
	s.Require().Len(s.voteChan, 1)
	vote = <-s.voteChan
	s.Equal(types.VotePreCom, vote.Type)
	s.Equal(blockHash, vote.BlockHash)
	s.Require().NoError(a.processSignal(
		s.prepareSignalByCopyingVote(agrPkg.SignalLock, vote)))
	a.nextState()
	// ForwardState
	s.Require().Len(s.voteChan, 1)
	vote = <-s.voteChan
	s.Equal(types.VoteCom, vote.Type)
	s.Equal(blockHash, vote.BlockHash)
	s.Equal(blockHash, a.data.lockValue)
	s.Equal(uint64(2), a.data.lockIter)
	s.Require().NoError(a.processSignal(
		s.prepareSignalByCopyingVote(agrPkg.SignalDecide, vote)))
	// We have enough of Com-Votes.
	s.Require().Len(s.confirmChan, 1)
	confirmBlock := <-s.confirmChan
	s.Equal(blockHash, confirmBlock)
}

func (s *AgreementTestSuite) TestPartitionOnCommitVote() {
	a, _ := s.newAgreement(4, -1, func(*types.Block) (bool, error) {
		return true, nil
	})
	// FastState
	a.nextState()
	// FastVoteState
	a.nextState()
	// InitialState
	a.nextState()
	// PreCommitState
	s.Require().Len(s.blockChan, 1)
	blockHash := <-s.blockChan
	block, exist := s.block[blockHash]
	s.Require().True(exist)
	s.Require().NoError(a.processBlock(block))
	s.Require().Len(s.voteChan, 1)
	vote := <-s.voteChan
	s.Equal(types.VoteInit, vote.Type)
	s.Equal(blockHash, vote.BlockHash)
	a.nextState()
	// CommitState
	s.Require().Len(s.voteChan, 1)
	vote = <-s.voteChan
	s.Equal(types.VotePreCom, vote.Type)
	s.Equal(blockHash, vote.BlockHash)
	s.Require().NoError(a.processSignal(
		s.prepareSignalByCopyingVote(agrPkg.SignalLock, vote)))
	a.nextState()
	// ForwardState
	s.Require().Len(s.voteChan, 1)
	vote = <-s.voteChan
	s.Equal(types.VoteCom, vote.Type)
	s.Equal(blockHash, vote.BlockHash)
	s.Equal(blockHash, a.data.lockValue)
	s.Equal(uint64(2), a.data.lockIter)
	// RepeateVoteState
	a.nextState()
	s.True(a.pullVotes())
	s.Require().Len(s.voteChan, 0)
}

func (s *AgreementTestSuite) TestFastConfirmLeader() {
	a, leaderNode := s.newAgreement(4, 0, func(*types.Block) (bool, error) {
		return true, nil
	})
	s.Require().Equal(s.ID, leaderNode)
	// FastState
	a.nextState()
	// FastVoteState
	s.Require().Len(s.blockChan, 1)
	blockHash := <-s.blockChan
	block, exist := s.block[blockHash]
	s.Require().True(exist)
	s.Require().Equal(s.ID, block.ProposerID)
	s.Require().NoError(a.processBlock(block))
	// Wait some time for go routine in processBlock to finish.
	time.Sleep(500 * time.Millisecond)
	s.Require().Len(s.voteChan, 1)
	vote := <-s.voteChan
	s.Equal(types.VoteFast, vote.Type)
	s.Equal(blockHash, vote.BlockHash)
	s.Require().Len(s.voteChan, 0)
	s.Require().NoError(a.processSignal(
		s.prepareSignalByCopyingVote(agrPkg.SignalLock, vote)))
	// We have enough of Fast-Votes.
	s.Require().Len(s.voteChan, 1)
	vote = <-s.voteChan
	s.Equal(types.VoteFastCom, vote.Type)
	s.Equal(blockHash, vote.BlockHash)
	s.Require().NoError(a.processSignal(
		s.prepareSignalByCopyingVote(agrPkg.SignalDecide, vote)))
	// We have enough of Fast-ConfirmVotes.
	s.Require().Len(s.confirmChan, 1)
	confirmBlock := <-s.confirmChan
	s.Equal(blockHash, confirmBlock)
}

func (s *AgreementTestSuite) TestFastConfirmNonLeader() {
	a, leaderNode := s.newAgreement(4, 1, func(*types.Block) (bool, error) {
		return true, nil
	})
	s.Require().NotEqual(s.ID, leaderNode)
	// FastState
	a.nextState()
	// FastVoteState
	s.Require().Len(s.blockChan, 0)
	block := s.proposeBlock(leaderNode, a.data.leader.hashCRS)
	s.Require().Equal(leaderNode, block.ProposerID)
	s.Require().NoError(a.processBlock(block))
	// Wait some time for go routine in processBlock to finish.
	time.Sleep(500 * time.Millisecond)
	var vote *types.Vote
	select {
	case vote = <-s.voteChan:
	case <-time.After(500 * time.Millisecond):
		s.FailNow("Should propose vote")
	}
	s.Equal(types.VoteFast, vote.Type)
	s.Equal(block.Hash, vote.BlockHash)
	s.Require().NoError(a.processSignal(
		s.prepareSignalByCopyingVote(agrPkg.SignalLock, vote)))
	// We have enough of Fast-Votes.
	s.Require().Len(s.voteChan, 1)
	vote = <-s.voteChan
	s.Require().NoError(a.processSignal(
		s.prepareSignalByCopyingVote(agrPkg.SignalDecide, vote)))
	// We have enough of Fast-ConfirmVotes.
	s.Require().Len(s.confirmChan, 1)
	confirmBlock := <-s.confirmChan
	s.Equal(block.Hash, confirmBlock)
}

func (s *AgreementTestSuite) TestFastForwardCond1() {
	a, _ := s.newAgreement(4, -1, func(*types.Block) (bool, error) {
		return true, nil
	})
	// No fast forward if those votes are from older period.
	a.data.lockIter = 1
	a.data.period = 3
	hash := common.NewRandomHash()
	s.Require().NoError(a.processSignal(s.prepareSignal(
		agrPkg.SignalLock, types.VotePreCom, hash, 2)))
	select {
	case <-a.done():
		s.FailNow("Unexpected fast forward.")
	default:
	}
	s.Equal(hash, a.data.lockValue)
	s.Equal(uint64(2), a.data.lockIter)
	s.Equal(uint64(3), a.data.period)

	// No fast forward if lockValue == vote.BlockHash.
	a.data.lockIter = 11
	a.data.period = 13
	a.data.lockValue = hash
	s.Require().NoError(a.processSignal(s.prepareSignal(
		agrPkg.SignalLock, types.VotePreCom, hash, 12)))
	select {
	case <-a.done():
		s.FailNow("Unexpected fast forward.")
	default:
	}
}

func (s *AgreementTestSuite) TestFastForwardCond2() {
	a, _ := s.newAgreement(4, -1, func(*types.Block) (bool, error) {
		return true, nil
	})
	a.data.period = 1
	hash := common.NewRandomHash()
	s.Require().NoError(a.processSignal(s.prepareSignal(
		agrPkg.SignalLock, types.VotePreCom, hash, 2)))
	select {
	case <-a.done():
	default:
		s.FailNow("Expecting fast forward.")
	}
	s.Equal(hash, a.data.lockValue)
	s.Equal(uint64(2), a.data.lockIter)
	s.Equal(uint64(2), a.data.period)
}

func (s *AgreementTestSuite) TestFastForwardCond3() {
	numVotes := 0
	a, _ := s.newAgreement(4, -1, func(*types.Block) (bool, error) {
		return true, nil
	})
	a.data.period = 1
	requiredVotes := len(s.signers)/3*2 + 1
	votes := make([]types.Vote, 0, requiredVotes)
	for nID := range s.signers {
		votes = append(votes, *s.prepareVote(
			nID, types.VoteCom, common.NewRandomHash(), uint64(2)))
		if numVotes++; numVotes == requiredVotes {
			break
		}
	}
	s.Require().NoError(a.processSignal(
		agrPkg.NewSignal(agrPkg.SignalForward, votes)))
	select {
	case <-a.done():
	default:
		s.FailNow("Expecting fast forward.")
	}
	s.Equal(uint64(3), a.data.period)

	s.Len(s.pulledBlocks, requiredVotes)
	for _, vote := range votes {
		_, exist := s.pulledBlocks[vote.BlockHash]
		s.True(exist)
	}
}

func (s *AgreementTestSuite) TestDecide() {
	a, _ := s.newAgreement(4, -1, func(*types.Block) (bool, error) {
		return true, nil
	})
	a.data.period = 5

	// No decide if com-vote on SKIP.
	s.Require().NoError(a.processSignal(s.prepareSignal(
		agrPkg.SignalForward, types.VoteCom, types.SkipBlockHash, 2)))
	s.Require().Len(s.confirmChan, 0)

	// Normal decide.
	hash := common.NewRandomHash()
	s.Require().NoError(a.processSignal(s.prepareSignal(
		agrPkg.SignalDecide, types.VoteCom, hash, 3)))
	s.Require().Len(s.confirmChan, 1)
	confirmBlock := <-s.confirmChan
	s.Equal(hash, confirmBlock)
}

func (s *AgreementTestSuite) TestForkBlock() {
	a, _ := s.newAgreement(4, -1, func(*types.Block) (bool, error) {
		return true, nil
	})
	for nID := range s.signers {
		b01 := s.proposeBlock(nID, a.data.leader.hashCRS)
		b02 := s.proposeBlock(nID, a.data.leader.hashCRS)
		s.Require().NoError(a.processBlock(b01))
		s.Require().IsType(&ErrFork{}, a.processBlock(b02))
		s.Require().Equal(b01.Hash, <-s.forkBlockChan)
		s.Require().Equal(b02.Hash, <-s.forkBlockChan)
	}
}

func (s *AgreementTestSuite) TestFindBlockInPendingSet() {
	a, leaderNode := s.newAgreement(4, 0, func(*types.Block) (bool, error) {
		return false, nil
	})
	block := s.proposeBlock(leaderNode, a.data.leader.hashCRS)
	s.Require().NoError(a.processBlock(block))
	// Make sure the block goes to pending pool in leader selector.
	block, exist := a.data.leader.findPendingBlock(block.Hash)
	s.Require().True(exist)
	s.Require().NotNil(block)
	// This block is allowed to be found by findBlockNoLock.
	block, exist = a.findBlockNoLock(block.Hash)
	s.Require().True(exist)
	s.Require().NotNil(block)
}

func TestAgreement(t *testing.T) {
	suite.Run(t, new(AgreementTestSuite))
}
