package node

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/athNdev/carbon-panel/cloud/internal/cloud/db"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/principal"
	"github.com/stretchr/testify/require"
)

func TestJoinHappyPathSingleUse(t *testing.T) {
	t.Parallel()
	s := openStore(t)
	ctx, _ := orgCtx(t, s)
	jt := NewJoinTokenService(deps(s))

	secret, tok, err := jt.Issue(ctx, IssueRequest{Name: "join-1", NodeTypeID: "small", Origin: "managed"})
	require.NoError(t, err)
	require.NotEmpty(t, secret)
	require.Contains(t, secret, "ccj_")
	require.NotEmpty(t, tok.ID)
	// Only the hash is stored; the secret itself is nowhere in the row.
	require.NotContains(t, tok.SecretHash, secret)
	require.NotEmpty(t, tok.SecretHash)

	got, err := jt.Redeem(context.Background(), secret)
	require.NoError(t, err)
	require.Equal(t, tok.ID, got.ID)
	require.NotNil(t, got.UsedAt)

	// Second redeem fails.
	_, err = jt.Redeem(context.Background(), secret)
	require.ErrorIs(t, err, ErrTokenRedeemed)
}

func TestJoinExpired(t *testing.T) {
	t.Parallel()
	s := openStore(t)
	ctx, _ := orgCtx(t, s)
	jt := NewJoinTokenService(deps(s))

	past := time.Now().UTC().Add(-time.Minute)
	secret, _, err := jt.Issue(ctx, IssueRequest{Name: "old", ExpiresAt: &past})
	require.NoError(t, err)
	_, err = jt.Redeem(context.Background(), secret)
	require.ErrorIs(t, err, ErrTokenExpired)
}

func TestJoinRevoked(t *testing.T) {
	t.Parallel()
	s := openStore(t)
	ctx, _ := orgCtx(t, s)
	jt := NewJoinTokenService(deps(s))

	secret, tok, err := jt.Issue(ctx, IssueRequest{Name: "rev"})
	require.NoError(t, err)
	_, err = jt.Revoke(ctx, tok.ID)
	require.NoError(t, err)
	_, err = jt.Redeem(context.Background(), secret)
	require.ErrorIs(t, err, ErrTokenRevoked)

	_, err = jt.Revoke(ctx, "missing")
	require.ErrorIs(t, err, ErrTokenUnknown)
}

func TestJoinTamperedAndUnknown(t *testing.T) {
	t.Parallel()
	s := openStore(t)
	ctx, _ := orgCtx(t, s)
	jt := NewJoinTokenService(deps(s))

	secret, _, err := jt.Issue(ctx, IssueRequest{Name: "tamper"})
	require.NoError(t, err)

	// Flip the final character of the signature.
	tampered := secret[:len(secret)-1] + flipLast(secret[len(secret)-1])
	_, err = jt.Redeem(context.Background(), tampered)
	require.ErrorIs(t, err, ErrTokenUnknown)

	for _, bad := range []string{"", "ccj_", "ccj_nodot", "junk", "ccj_../..x.y"} {
		_, err = jt.Redeem(context.Background(), bad)
		require.ErrorIs(t, err, ErrTokenUnknown, bad)
	}
}

func flipLast(c byte) string {
	if c == 'A' {
		return "B"
	}
	return "A"
}

func TestJoinOrgMismatch(t *testing.T) {
	t.Parallel()
	s := openStore(t)
	ctxA, _ := orgCtx(t, s)
	ctxB, _ := orgCtx(t, s)
	jt := NewJoinTokenService(deps(s))

	secret, _, err := jt.Issue(ctxA, IssueRequest{Name: "org"})
	require.NoError(t, err)

	// No-org context (the node-join path) succeeds.
	_, err = jt.Redeem(context.Background(), secret)
	require.NoError(t, err)

	secret2, _, err := jt.Issue(ctxA, IssueRequest{Name: "org2"})
	require.NoError(t, err)
	_, err = jt.Redeem(ctxB, secret2)
	require.ErrorIs(t, err, ErrTokenOrg)
}

func TestJoinConcurrentRedeem(t *testing.T) {
	t.Parallel()
	s := openStore(t)
	ctx, _ := orgCtx(t, s)
	jt := NewJoinTokenService(deps(s))

	secret, _, err := jt.Issue(ctx, IssueRequest{Name: "race"})
	require.NoError(t, err)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = jt.Redeem(context.Background(), secret)
		}(i)
	}
	wg.Wait()

	succeeded := 0
	for _, e := range errs {
		if e == nil {
			succeeded++
		} else {
			require.ErrorIs(t, e, ErrTokenRedeemed)
		}
	}
	require.Equal(t, 1, succeeded, "exactly one redeemer must win")
}

func TestJoinNoOrgAndNoPepperRefuse(t *testing.T) {
	t.Parallel()
	s := openStore(t)
	jt := NewJoinTokenService(deps(s))

	_, _, err := jt.Issue(context.Background(), IssueRequest{Name: "x"})
	require.ErrorIs(t, err, db.ErrNoOrg)

	// No pepper anywhere refuses instead of signing weakly.
	bare := NewJoinTokenService(Deps{Store: s})
	ctx, _ := orgCtx(t, s)
	_, _, err = bare.Issue(ctx, IssueRequest{Name: "x"})
	require.ErrorIs(t, err, ErrPepperMissing)
	_, err = bare.Redeem(ctx, "ccj_whatever.whatever")
	require.ErrorIs(t, err, ErrPepperMissing)
}

func TestJoinListRevokedScoped(t *testing.T) {
	t.Parallel()
	s := openStore(t)
	ctxA, orgA := orgCtx(t, s)
	jt := NewJoinTokenService(deps(s))

	_, _, err := jt.Issue(ctxA, IssueRequest{Name: "a1"})
	require.NoError(t, err)
	_, _, err = jt.Issue(ctxA, IssueRequest{Name: "a2"})
	require.NoError(t, err)

	list, err := jt.List(ctxA)
	require.NoError(t, err)
	require.Len(t, list, 2)

	other := principal.WithPrincipal(context.Background(), principal.Principal{OrgID: orgA + "-other"})
	otherList, err := jt.List(other)
	require.NoError(t, err)
	require.Empty(t, otherList)

	_, err = jt.List(context.Background())
	require.ErrorIs(t, err, db.ErrNoOrg)
}
