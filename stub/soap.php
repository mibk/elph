<?php

class SoapClient
{
	function __setSoapHeaders(SoapHeader|array|null $headers = null): bool;

	function __soapCall(
		string $name,
		array $args,
		?array $options = null,
		SoapHeader|array|null $inputHeaders = null,
		array &$outputHeaders = null,
	): mixed;
}

class SoapHeader
{
	public string $namespace;
	public string $name;
	public mixed $data = null;
	public bool $mustUnderstand;
	public string|int|null $actor;
}

class SoapVar
{
	public int $enc_type;
	public mixed $enc_value    = null;
	public ?string $enc_stype  = null;
	public ?string $enc_ns     = null;
	public ?string $enc_name   = null;
	public ?string $enc_namens = null;
}

class SoapFault extends Exception
{
}
