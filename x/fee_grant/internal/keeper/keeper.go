package keeper

import (
	"fmt"

	"github.com/tendermint/tendermint/libs/log"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/fee_grant/exported"
	"github.com/cosmos/cosmos-sdk/x/fee_grant/internal/types"
)

// Keeper manages state of all fee grants, as well as calculating approval.
// It must have a codec with all available allowances registered.
type Keeper struct {
	cdc      *codec.Codec
	storeKey sdk.StoreKey
}

// NewKeeper creates a fee grant Keeper
func NewKeeper(cdc *codec.Codec, storeKey sdk.StoreKey) Keeper {
	return Keeper{cdc: cdc, storeKey: storeKey}
}

// Logger returns a module-specific logger.
func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", fmt.Sprintf("x/%s", types.ModuleName))
}

// GrantFeeAllowance creates a new grant
func (k Keeper) GrantFeeAllowance(ctx sdk.Context, grant types.FeeAllowanceGrant) {
	store := ctx.KVStore(k.storeKey)
	key := types.FeeAllowanceKey(grant.Granter, grant.Grantee)
	bz := k.cdc.MustMarshalBinaryBare(grant)
	store.Set(key, bz)
}

// RevokeFeeAllowance removes an existing grant
func (k Keeper) RevokeFeeAllowance(ctx sdk.Context, granter, grantee sdk.AccAddress) {
	store := ctx.KVStore(k.storeKey)
	key := types.FeeAllowanceKey(granter, grantee)
	store.Delete(key)
}

// GetFeeAllowance returns the allowance between the granter and grantee.
// If there is none, it returns nil, nil.
// Returns an error on parsing issues
func (k Keeper) GetFeeAllowance(ctx sdk.Context, granter, grantee sdk.AccAddress) exported.FeeAllowance {
	grant, found := k.GetFeeGrant(ctx, granter, grantee)
	if !found {
		return nil
	}
	return grant.Allowance
}

// GetFeeGrant returns entire grant between both accounts
func (k Keeper) GetFeeGrant(ctx sdk.Context, granter sdk.AccAddress, grantee sdk.AccAddress) (types.FeeAllowanceGrant, bool) {
	store := ctx.KVStore(k.storeKey)
	key := types.FeeAllowanceKey(granter, grantee)
	bz := store.Get(key)
	if len(bz) == 0 {
		return types.FeeAllowanceGrant{}, false
	}

	var grant types.FeeAllowanceGrant
	k.cdc.MustUnmarshalBinaryBare(bz, &grant)
	return grant, true
}

// IterateAllGranteeFeeAllowances iterates over all the grants from anyone to the given grantee.
// Callback to get all data, returns true to stop, false to keep reading
func (k Keeper) IterateAllGranteeFeeAllowances(ctx sdk.Context, grantee sdk.AccAddress, cb func(types.FeeAllowanceGrant) bool) error {
	store := ctx.KVStore(k.storeKey)
	prefix := types.FeeAllowancePrefixByGrantee(grantee)
	iter := sdk.KVStorePrefixIterator(store, prefix)
	defer iter.Close()

	stop := false
	for ; iter.Valid() && !stop; iter.Next() {
		bz := iter.Value()
		var grant types.FeeAllowanceGrant
		err := k.cdc.UnmarshalBinaryBare(bz, &grant)
		if err != nil {
			return err
		}
		stop = cb(grant)
	}
	return nil
}

// IterateAllFeeAllowances iterates over all the grants in the store.
// Callback to get all data, returns true to stop, false to keep reading
// Calling this without pagination is very expensive and only designed for export genesis
func (k Keeper) IterateAllFeeAllowances(ctx sdk.Context, cb func(types.FeeAllowanceGrant) bool) error {
	store := ctx.KVStore(k.storeKey)
	iter := sdk.KVStorePrefixIterator(store, types.FeeAllowanceKeyPrefix)
	defer iter.Close()

	stop := false
	for ; iter.Valid() && !stop; iter.Next() {
		bz := iter.Value()
		var grant types.FeeAllowanceGrant
		err := k.cdc.UnmarshalBinaryBare(bz, &grant)
		if err != nil {
			return err
		}
		stop = cb(grant)
	}
	return nil
}

// UseGrantedFees will try to pay the given fee from the granter's account as requested by the grantee
func (k Keeper) UseGrantedFees(ctx sdk.Context, granter, grantee sdk.AccAddress, fee sdk.Coins) error {
	grant, found := k.GetFeeGrant(ctx, granter, grantee)
	if !found || grant.Allowance == nil {
		return sdkerrors.Wrapf(types.ErrNoAllowance, "grant missing")
	}

	remove, err := grant.Allowance.Accept(fee, ctx.BlockTime(), ctx.BlockHeight())
	if remove {
		k.RevokeFeeAllowance(ctx, granter, grantee)
		// note this returns nil if err == nil
		return sdkerrors.Wrap(err, "removed grant")
	}
	if err != nil {
		return sdkerrors.Wrap(err, "invalid grant")
	}

	// if we accepted, store the updated state of the allowance
	k.GrantFeeAllowance(ctx, grant)
	return nil
}
