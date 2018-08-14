// Copyright 2018 The dexon-consensus-core Authors
// This file is part of the dexon-consensus-core library.
//
// The dexon-consensus-core library is free software: you can redistribute it
// and/or modify it under the terms of the GNU Lesser General Public License as
// published by the Free Software Foundation, either version 3 of the License,
// or (at your option) any later version.
//
// The dexon-consensus-core library is distributed in the hope that it will be
// useful, but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU Lesser
// General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the dexon-consensus-core library. If not, see
// <http://www.gnu.org/licenses/>.

package core

import (
	"sort"
	"testing"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/blockdb"
	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/test"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/dexon-foundation/dexon-consensus-core/crypto/eth"
	"github.com/stretchr/testify/suite"
)

type ConsensusTestSuite struct {
	suite.Suite
}

func (s *ConsensusTestSuite) prepareGenesisBlock(
	proposerID types.ValidatorID,
	gov Governance) *types.Block {

	block := &types.Block{
		ProposerID: proposerID,
		ParentHash: common.Hash{},
		Height:     0,
		Acks:       make(map[common.Hash]struct{}),
		Timestamps: make(map[types.ValidatorID]time.Time),
	}
	for vID := range gov.GetValidatorSet() {
		block.Timestamps[vID] = time.Time{}
	}
	block.Timestamps[proposerID] = time.Now().UTC()
	var err error
	block.Hash, err = hashBlock(block)
	s.Require().Nil(err)
	return block
}

func (s *ConsensusTestSuite) prepareConsensus(gov *test.Governance) (
	*test.App, *Consensus) {

	app := test.NewApp()
	db, err := blockdb.NewMemBackedBlockDB()
	s.Require().Nil(err)
	prv, err := eth.NewPrivateKey()
	s.Require().Nil(err)
	con := NewConsensus(app, gov, db, prv, eth.SigToPub)
	return app, con
}

func (s *ConsensusTestSuite) TestSimpleDeliverBlock() {
	// This test scenario:
	// o o o o <- this layer makes older blocks strongly acked.
	// |x|x|x| <- lots of acks.
	// o | o o <- this layer would be sent to total ordering.
	// |\|/|-|
	// | o | | <- the only block which is acked by all other blocks
	// |/|\|\|    at the same height.
	// o o o o <- genesis blocks
	// 0 1 2 3 <- index of validator ID
	//
	// This test case only works for Total Ordering with K=0.
	var (
		minInterval = 50 * time.Millisecond
		gov         = test.NewGovernance(4, 1000)
		req         = s.Require()
		validators  []types.ValidatorID
	)

	for vID := range gov.GetValidatorSet() {
		validators = append(validators, vID)
	}

	// Setup core.Consensus and test.App.
	objs := map[types.ValidatorID]*struct {
		app *test.App
		con *Consensus
	}{}
	for _, vID := range validators {
		app, con := s.prepareConsensus(gov)
		objs[vID] = &struct {
			app *test.App
			con *Consensus
		}{app, con}
	}
	// It's a helper function to emit one block
	// to all core.Consensus objects.
	broadcast := func(b *types.Block) {
		for _, obj := range objs {
			req.Nil(obj.con.ProcessBlock(b))
		}
	}
	// Genesis blocks
	b00 := s.prepareGenesisBlock(validators[0], gov)
	time.Sleep(minInterval)
	b10 := s.prepareGenesisBlock(validators[1], gov)
	time.Sleep(minInterval)
	b20 := s.prepareGenesisBlock(validators[2], gov)
	time.Sleep(minInterval)
	b30 := s.prepareGenesisBlock(validators[3], gov)
	broadcast(b00)
	broadcast(b10)
	broadcast(b20)
	broadcast(b30)
	// Setup b11.
	time.Sleep(minInterval)
	b11 := &types.Block{
		ProposerID: validators[1],
	}
	var err error
	b11.Hash, err = hashBlock(b11)
	s.Require().Nil(err)
	req.Nil(objs[validators[1]].con.PrepareBlock(b11, time.Now().UTC()))
	req.Len(b11.Acks, 4)
	req.Contains(b11.Acks, b00.Hash)
	req.Contains(b11.Acks, b10.Hash)
	req.Contains(b11.Acks, b20.Hash)
	req.Contains(b11.Acks, b30.Hash)
	broadcast(b11)
	// Setup b01.
	time.Sleep(minInterval)
	b01 := &types.Block{
		ProposerID: validators[0],
		Hash:       common.NewRandomHash(),
	}
	req.Nil(objs[validators[0]].con.PrepareBlock(b01, time.Now().UTC()))
	req.Len(b01.Acks, 4)
	req.Contains(b01.Acks, b11.Hash)
	// Setup b21.
	time.Sleep(minInterval)
	b21 := &types.Block{
		ProposerID: validators[2],
		Hash:       common.NewRandomHash(),
	}
	req.Nil(objs[validators[2]].con.PrepareBlock(b21, time.Now().UTC()))
	req.Len(b21.Acks, 4)
	req.Contains(b21.Acks, b11.Hash)
	// Setup b31.
	time.Sleep(minInterval)
	b31 := &types.Block{
		ProposerID: validators[3],
		Hash:       common.NewRandomHash(),
	}
	req.Nil(objs[validators[3]].con.PrepareBlock(b31, time.Now().UTC()))
	req.Len(b31.Acks, 4)
	req.Contains(b31.Acks, b11.Hash)
	// Broadcast other height=1 blocks.
	broadcast(b01)
	broadcast(b21)
	broadcast(b31)
	// Setup height=2 blocks.
	// Setup b02.
	time.Sleep(minInterval)
	b02 := &types.Block{
		ProposerID: validators[0],
		Hash:       common.NewRandomHash(),
	}
	req.Nil(objs[validators[0]].con.PrepareBlock(b02, time.Now().UTC()))
	req.Len(b02.Acks, 3)
	req.Contains(b02.Acks, b01.Hash)
	req.Contains(b02.Acks, b21.Hash)
	req.Contains(b02.Acks, b31.Hash)
	// Setup b12.
	time.Sleep(minInterval)
	b12 := &types.Block{
		ProposerID: validators[1],
		Hash:       common.NewRandomHash(),
	}
	req.Nil(objs[validators[1]].con.PrepareBlock(b12, time.Now().UTC()))
	req.Len(b12.Acks, 4)
	req.Contains(b12.Acks, b01.Hash)
	req.Contains(b12.Acks, b11.Hash)
	req.Contains(b12.Acks, b21.Hash)
	req.Contains(b12.Acks, b31.Hash)
	// Setup b22.
	time.Sleep(minInterval)
	b22 := &types.Block{
		ProposerID: validators[2],
		Hash:       common.NewRandomHash(),
	}
	req.Nil(objs[validators[2]].con.PrepareBlock(b22, time.Now().UTC()))
	req.Len(b22.Acks, 3)
	req.Contains(b22.Acks, b01.Hash)
	req.Contains(b22.Acks, b21.Hash)
	req.Contains(b22.Acks, b31.Hash)
	// Setup b32.
	time.Sleep(minInterval)
	b32 := &types.Block{
		ProposerID: validators[3],
		Hash:       common.NewRandomHash(),
	}
	req.Nil(objs[validators[3]].con.PrepareBlock(b32, time.Now().UTC()))
	req.Len(b32.Acks, 3)
	req.Contains(b32.Acks, b01.Hash)
	req.Contains(b32.Acks, b21.Hash)
	req.Contains(b32.Acks, b31.Hash)
	// Broadcast blocks at height=2.
	broadcast(b02)
	broadcast(b12)
	broadcast(b22)
	broadcast(b32)

	// Verify the cached status of each app.
	verify := func(app *test.App) {
		// Check blocks that are strongly acked.
		req.Contains(app.Acked, b00.Hash)
		req.Contains(app.Acked, b10.Hash)
		req.Contains(app.Acked, b20.Hash)
		req.Contains(app.Acked, b30.Hash)
		req.Contains(app.Acked, b01.Hash)
		req.Contains(app.Acked, b11.Hash)
		req.Contains(app.Acked, b21.Hash)
		req.Contains(app.Acked, b31.Hash)
		// Genesis blocks are delivered by total ordering as a set.
		delivered0 := common.Hashes{b00.Hash, b10.Hash, b20.Hash, b30.Hash}
		sort.Sort(delivered0)
		req.Len(app.TotalOrdered, 2)
		req.Equal(app.TotalOrdered[0].BlockHashes, delivered0)
		req.False(app.TotalOrdered[0].Early)
		// b11 is the sencond set delivered by total ordering.
		delivered1 := common.Hashes{b11.Hash}
		sort.Sort(delivered1)
		req.Equal(app.TotalOrdered[1].BlockHashes, delivered1)
		req.False(app.TotalOrdered[1].Early)
		// Check generated timestamps.
		req.Contains(app.Delivered, b00.Hash)
		req.Contains(app.Delivered, b10.Hash)
		req.Contains(app.Delivered, b20.Hash)
		req.Contains(app.Delivered, b30.Hash)
		req.Contains(app.Delivered, b11.Hash)
		// Check timestamps, there is no direct way to know which block is
		// selected as main chain, we can only detect it by making sure
		// its ConsensusTimestamp is not interpolated.
		t, err := getMedianTime(b11)
		req.Nil(err)
		req.Equal(t, app.Delivered[b11.Hash])
	}
	for _, obj := range objs {
		verify(obj.app)
	}
}

func (s *ConsensusTestSuite) TestPrepareBlock() {
	// This test case would test these steps:
	//  - Add all genesis blocks into lattice.
	//  - Make sure Consensus.PrepareBlock would attempt to ack
	//    all genesis blocks.
	//  - Add the prepared block into lattice.
	//  - Make sure Consensus.PrepareBlock would only attempt to
	//    ack the prepared block.
	var (
		gov        = test.NewGovernance(4, 1000)
		req        = s.Require()
		validators []types.ValidatorID
	)
	for vID := range gov.GetValidatorSet() {
		validators = append(validators, vID)
	}
	_, con := s.prepareConsensus(gov)
	b00 := s.prepareGenesisBlock(validators[0], gov)
	b10 := s.prepareGenesisBlock(validators[1], gov)
	b20 := s.prepareGenesisBlock(validators[2], gov)
	b30 := s.prepareGenesisBlock(validators[3], gov)
	req.Nil(con.ProcessBlock(b00))
	req.Nil(con.ProcessBlock(b10))
	req.Nil(con.ProcessBlock(b20))
	req.Nil(con.ProcessBlock(b30))
	b11 := &types.Block{
		ProposerID: validators[1],
	}
	var err error
	b11.Hash, err = hashBlock(b11)
	s.Require().Nil(err)
	// Sleep to make sure 'now' is slower than b10's timestamp.
	time.Sleep(100 * time.Millisecond)
	req.Nil(con.PrepareBlock(b11, time.Now().UTC()))
	// Make sure we would assign 'now' to the timestamp belongs to
	// the proposer.
	req.True(
		b11.Timestamps[validators[1]].Sub(
			b10.Timestamps[validators[1]]) > 100*time.Millisecond)
	req.Nil(con.ProcessBlock(b11))
	b12 := &types.Block{
		ProposerID: validators[1],
	}
	b12.Hash, err = hashBlock(b12)
	s.Require().Nil(err)
	req.Nil(con.PrepareBlock(b12, time.Now().UTC()))
	req.Len(b12.Acks, 1)
	req.Contains(b12.Acks, b11.Hash)
}

func TestConsensus(t *testing.T) {
	suite.Run(t, new(ConsensusTestSuite))
}