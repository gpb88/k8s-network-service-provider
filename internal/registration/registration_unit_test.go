package registration_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/dcm-project/k8s-network-service-provider/internal/config"
	"github.com/dcm-project/k8s-network-service-provider/internal/registration"
)

var _ = Describe("Registration Payload", func() {
	It("contains network service type, networks endpoint, and CRUD operations", func() {
		cfg := &config.Config{
			Provider: config.ProviderConfig{
				Name:        "k8s-network-sp",
				DisplayName: "K8s Network SP",
				Endpoint:    "https://sp.example.com",
			},
		}

		payload := registration.BuildPayload(cfg)

		Expect(payload.Name).To(Equal("k8s-network-sp"))
		Expect(payload.ServiceType).To(Equal("network"))
		Expect(payload.DisplayName).To(HaveValue(Equal("K8s Network SP")))
		Expect(payload.Endpoint).To(Equal("https://sp.example.com/api/v1alpha1/networks"))
		Expect(payload.Operations).To(HaveValue(ConsistOf("CREATE", "READ", "DELETE")))
		Expect(payload.SchemaVersion).To(Equal("v1alpha1"))
	})
})
