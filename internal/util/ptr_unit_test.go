package util_test

import (
	"github.com/dcm-project/k8s-network-service-provider/internal/util"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Ptr", func() {
	It("returns a pointer to a string value", func() {
		s := "hello"
		p := util.Ptr(s)
		Expect(p).NotTo(BeNil())
		Expect(*p).To(Equal("hello"))
	})

	It("returns a pointer to an integer value", func() {
		i := 42
		p := util.Ptr(i)
		Expect(p).NotTo(BeNil())
		Expect(*p).To(Equal(42))
	})

	It("returns a pointer to a boolean value", func() {
		b := true
		p := util.Ptr(b)
		Expect(p).NotTo(BeNil())
		Expect(*p).To(BeTrue())
	})
})
