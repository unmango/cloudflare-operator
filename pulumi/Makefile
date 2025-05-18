VERSION ?= v0.0.1-alpha1

CRD2PULUMI ?= go tool crd2pulumi
SDKS := dotnet nodejs

all: ${SDKS}

export DOTNET_NAME := Pulumi.Cloudflare.Operator
export NODEJS_NAME := pulumi-cloudflare-operator

.PHONY: ${SDKS}
${SDKS}: %:
	rm -rf $* && $(CRD2PULUMI) --$* \
		--$*Name $$$(shell echo $* | tr a-z A-Z)_NAME \
		--$*Path ${CURDIR}/$* \
		--version ${VERSION} \
		$(wildcard ../config/crd/bases/*)
	${CURDIR}/patch-$*.sh
