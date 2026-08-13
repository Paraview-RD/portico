package auth

import "golang.org/x/crypto/bcrypt"

// CurrentHashCost and BurnComparisonCost expose what the package hashes with
// and what its throwaway comparison costs, so a test can require the two to
// agree.
//
// In a _test.go file, so neither exists in a build of the product.
func CurrentHashCost() int { return hashCost }

func BurnComparisonCost() int {
	cost, err := bcrypt.Cost(dummyHash())
	if err != nil {
		panic("auth: the comparison hash is not a bcrypt hash: " + err.Error())
	}
	return cost
}
