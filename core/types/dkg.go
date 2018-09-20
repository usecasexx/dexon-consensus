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

package types

import (
	"github.com/dexon-foundation/dexon-consensus-core/crypto"
	"github.com/dexon-foundation/dexon-consensus-core/crypto/dkg"
)

// DKGPrivateShare describe a secret share in DKG protocol.
type DKGPrivateShare struct {
	ProposerID   NodeID           `json:"proposer_id"`
	Round        uint64           `json:"round"`
	PrivateShare dkg.PrivateKey   `json:"private_share"`
	Signature    crypto.Signature `json:"signature"`
}

// DKGMasterPublicKey decrtibe a master public key in DKG protocol.
type DKGMasterPublicKey struct {
	ProposerID      NodeID              `json:"proposer_id"`
	Round           uint64              `json:"round"`
	DKGID           dkg.ID              `json:"dkg_id"`
	PublicKeyShares dkg.PublicKeyShares `json:"public_key_shares"`
	Signature       crypto.Signature    `json:"signature"`
}

// DKGComplaint describe a complaint in DKG protocol.
type DKGComplaint struct {
	ProposerID   NodeID           `json:"proposer_id"`
	Round        uint64           `json:"round"`
	PrivateShare DKGPrivateShare  `json:"private_share"`
	Signature    crypto.Signature `json:"signature"`
}

// DKGPartialSignature describe a partial signature in DKG protocol.
type DKGPartialSignature struct {
	ProposerID       NodeID               `json:"proposerID"`
	Round            uint64               `json:"round"`
	PartialSignature dkg.PartialSignature `json:"partial_signature"`
	Signature        crypto.Signature     `json:"signature"`
}