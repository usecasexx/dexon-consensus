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

package test

import (
	"context"
	"encoding/json"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/crypto"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	typesDKG "github.com/dexon-foundation/dexon-consensus/core/types/dkg"
	"github.com/dexon-foundation/dexon-consensus/core/utils"
	"github.com/stretchr/testify/suite"
)

type NetworkTestSuite struct {
	suite.Suite
}

func (s *NetworkTestSuite) setupNetworks(
	pubKeys []crypto.PublicKey) map[types.NodeID]*Network {
	var (
		server = NewFakeTransportServer()
		wg     sync.WaitGroup
	)
	serverChannel, err := server.Host()
	s.Require().NoError(err)
	// Setup several network modules.
	networks := make(map[types.NodeID]*Network)
	for _, key := range pubKeys {
		n := NewNetwork(
			key,
			&FixedLatencyModel{},
			NewDefaultMarshaller(nil),
			NetworkConfig{Type: NetworkTypeFake})
		networks[n.ID] = n
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Require().NoError(n.Setup(serverChannel))
			go n.Run()
		}()
	}
	s.Require().NoError(server.WaitForPeers(uint32(len(pubKeys))))
	wg.Wait()
	return networks
}

func (s *NetworkTestSuite) TestPullRequestMarshaling() {
	// Verify pull request for blocks is able to be marshalled.
	blockHashes := common.Hashes{
		common.NewRandomHash(),
		common.NewRandomHash(),
		common.NewRandomHash(),
	}
	req := &PullRequest{
		Requester: GenerateRandomNodeIDs(1)[0],
		Type:      "block",
		Identity:  blockHashes,
	}
	b, err := json.Marshal(req)
	s.Require().NoError(err)
	req2 := &PullRequest{}
	s.Require().NoError(json.Unmarshal(b, req2))
	s.Require().Equal(req.Requester, req2.Requester)
	s.Require().Equal(req.Type, req2.Type)
	s.Require().Equal(blockHashes, req2.Identity)
	// Verify pull request for votes is able to be marshalled.
	req = &PullRequest{
		Requester: GenerateRandomNodeIDs(1)[0],
		Type:      "vote",
		Identity: types.Position{
			Round:   1,
			ChainID: 2,
			Height:  3,
		}}
	b, err = json.Marshal(req)
	s.Require().NoError(err)
	req2 = &PullRequest{}
	s.Require().NoError(json.Unmarshal(b, req2))
	s.Require().Equal(req.Requester, req2.Requester)
	s.Require().Equal(req.Type, req2.Type)
	s.Require().Equal(req.Identity.(types.Position).Round,
		req.Identity.(types.Position).Round)
	s.Require().Equal(req.Identity.(types.Position).ChainID,
		req.Identity.(types.Position).ChainID)
	s.Require().Equal(req.Identity.(types.Position).Height,
		req.Identity.(types.Position).Height)
}

func (s *NetworkTestSuite) TestPullBlocks() {
	var (
		peerCount = 10
		req       = s.Require()
	)
	_, pubKeys, err := NewKeys(peerCount)
	req.NoError(err)
	networks := s.setupNetworks(pubKeys)
	// Generate several random hashes.
	hashes := common.Hashes{}
	for range networks {
		hashes = append(hashes, common.NewRandomHash())
	}
	// Randomly pick one network instance as master.
	var master *Network
	for _, master = range networks {
		break
	}
	// Send a fake block to a random network (except master) by those hashes.
	for _, h := range hashes {
		for _, n := range networks {
			if n.ID == master.ID {
				continue
			}
			req.NoError(master.trans.Send(n.ID, &types.Block{Hash: h}))
		}
	}
	// Make sure each node receive their blocks.
	time.Sleep(1 * time.Second)
	// Initiate a pull request from network 0 by removing corresponding hash in
	// hashes.
	master.PullBlocks(hashes)
	awaitMap := make(map[common.Hash]struct{})
	for _, h := range hashes {
		awaitMap[h] = struct{}{}
	}
	// We should be able to receive all hashes.
	ctx, cancelFunc := context.WithTimeout(context.Background(), 3*time.Second)
	defer func() { cancelFunc() }()
	for {
		select {
		case v := <-master.ReceiveChan():
			b, ok := v.(*types.Block)
			if !ok {
				break
			}
			delete(awaitMap, b.Hash)
		case <-ctx.Done():
			// This test case fails, we didn't receive pulled blocks.
			req.False(true)
		}
		if len(awaitMap) == 0 {
			break
		}
	}
}

func (s *NetworkTestSuite) TestPullVotes() {
	// The functionality of pulling votes is not deterministic, so the test here
	// only tries to "retry pulling votes until we can get some votes back".
	var (
		peerCount     = 10
		maxRound      = uint64(5)
		voteCount     = 200
		voteTestCount = 15
		req           = s.Require()
	)
	_, pubKeys, err := NewKeys(peerCount)
	req.NoError(err)
	networks := s.setupNetworks(pubKeys)
	// Randomly pick one network instance as master.
	var master *Network
	for _, master = range networks {
		break
	}
	// Prepare notary sets.
	notarySets := []map[types.NodeID]struct{}{}
	for i := uint64(0); i <= maxRound; i++ {
		notarySets = append(notarySets, make(map[types.NodeID]struct{}))
	}
	// Randomly generate votes to random peers, except master.
	votes := make(map[types.VoteHeader]*types.Vote)
	randObj := rand.New(rand.NewSource(time.Now().UnixNano()))
	for len(votes) < voteCount {
		for _, n := range networks {
			if n.ID == master.ID {
				continue
			}
			v := types.NewVote(
				types.VoteInit, common.NewRandomHash(), randObj.Uint64())
			v.Position = types.Position{
				ChainID: randObj.Uint32(),
				Height:  randObj.Uint64(),
				Round:   uint64(randObj.Intn(int(maxRound + 1))),
			}
			req.NoError(master.trans.Send(n.ID, v))
			votes[v.VoteHeader] = v
			// Add this node to corresponding notary set for this vote.
			notarySets[v.Position.Round][n.ID] = struct{}{}
		}
	}
	// Randomly generate votes set to test.
	votesToTest := make(map[types.VoteHeader]struct{})
	for len(votesToTest) < voteTestCount {
		// Randomly pick a vote
		for _, v := range votes {
			votesToTest[v.VoteHeader] = struct{}{}
			break
		}
	}
	// Try to pull all votes with timeout.
	ctx, cancelFunc := context.WithTimeout(context.Background(), 3*time.Second)
	defer func() { cancelFunc() }()
	for len(votesToTest) > 0 {
		for vHeader := range votesToTest {
			master.PullVotes(vHeader.Position)
			break
		}
		select {
		case v := <-master.ReceiveChan():
			vv, ok := v.(*types.Vote)
			if !ok {
				break
			}
			delete(votesToTest, vv.VoteHeader)
		case <-ctx.Done():
			req.True(false)
		default:
		}
	}
}

func (s *NetworkTestSuite) TestBroadcastToSet() {
	// Make sure when a network module attached to a utils.NodeSetCache,
	// These function would broadcast to correct nodes, not all peers.
	//  - BroadcastVote, notary set.
	//  - BroadcastBlock, notary set.
	//  - BroadcastDKGPrivateShare, DKG set.
	//  - BroadcastDKGPartialSignature, DKG set.
	var (
		req       = s.Require()
		peerCount = 5
	)
	_, pubKeys, err := NewKeys(peerCount)
	req.NoError(err)
	gov, err := NewGovernance(pubKeys, time.Second, 2)
	req.NoError(err)
	req.NoError(gov.State().RequestChange(StateChangeDKGSetSize, uint32(1)))
	req.NoError(gov.State().RequestChange(StateChangeNotarySetSize, uint32(1)))
	networks := s.setupNetworks(pubKeys)
	cache := utils.NewNodeSetCache(gov)
	// Cache required set of nodeIDs.
	dkgSet, err := cache.GetDKGSet(0)
	req.NoError(err)
	req.Len(dkgSet, 1)
	notarySet, err := cache.GetNotarySet(0, 0)
	req.NoError(err)
	req.Len(notarySet, 1)
	var (
		// Some node don't belong to any set.
		nerd                *Network
		dkgNode, notaryNode *Network
	)
	for nID, n := range networks {
		if _, exists := dkgSet[nID]; exists {
			continue
		}
		if _, exists := notarySet[nID]; exists {
			continue
		}
		nerd = n
		break
	}
	for nID := range dkgSet {
		dkgNode = networks[nID]
		break
	}
	for nID := range notarySet {
		notaryNode = networks[nID]
		break
	}
	req.NotNil(nerd)
	req.NotNil(dkgNode)
	req.NotNil(notaryNode)
	nerd.AddNodeSetCache(cache)
	// Try broadcasting with datum from round 0, and make sure only node belongs
	// to that set receiving the message.
	nerd.BroadcastVote(&types.Vote{})
	req.IsType(<-notaryNode.ReceiveChan(), &types.Vote{})
	nerd.BroadcastBlock(&types.Block{})
	req.IsType(<-notaryNode.ReceiveChan(), &types.Block{})
	nerd.BroadcastDKGPrivateShare(&typesDKG.PrivateShare{})
	req.IsType(<-dkgNode.ReceiveChan(), &typesDKG.PrivateShare{})
	nerd.BroadcastDKGPartialSignature(&typesDKG.PartialSignature{})
	req.IsType(<-dkgNode.ReceiveChan(), &typesDKG.PartialSignature{})
	// There should be no remaining message in each node.
	for _, n := range networks {
		select {
		case <-n.ReceiveChan():
			req.False(true)
		default:
		}
	}
}

func TestNetwork(t *testing.T) {
	suite.Run(t, new(NetworkTestSuite))
}
