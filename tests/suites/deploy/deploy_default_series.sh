run_deploy_default_series() {
	echo

	model_name="test-deploy-default-series"
	file="${TEST_DIR}/${model_name}.log"

	ensure "${model_name}" "${file}"

	juju model-config default-series=focal
	juju deploy ubuntu

	ubuntu_base_name=$(juju status --format=json | jq ".applications.ubuntu.base.name")
	ubuntu_base_ch=$(juju status --format=json | jq ".applications.ubuntu.base.channel")
	echo "$ubuntu_base_name" | check "ubuntu"
	echo "$ubuntu_base_ch" | check "20.04"

	destroy_model "${model_name}"
}

run_deploy_not_default_series() {
	echo

	model_name="test-deploy-not-default-series"
	file="${TEST_DIR}/${model_name}.log"

	ensure "${model_name}" "${file}"

	juju model-config default-series=focal
	juju deploy ubuntu --base ubuntu@22.04

	ubuntu_base_name=$(juju status --format=json | jq ".applications.ubuntu.base.name")
	ubuntu_base_ch=$(juju status --format=json | jq ".applications.ubuntu.base.channel")
	echo "$ubuntu_base_name" | check "ubuntu"
	echo "$ubuntu_base_ch" | check "22.04"

	destroy_model "${model_name}"
}

test_deploy_default_series() {
	if [ "$(skip 'test_deploy_default_series')" ]; then
		echo "==> TEST SKIPPED: deploy default series"
		return
	fi

	(
		set_verbosity

		cd .. || exit

		run "run_deploy_default_series"
		run "run_deploy_not_default_series"
	)
}
