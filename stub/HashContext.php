<?php

final class HashContext
{
	function __serialize(): array;
	function __unserialize(array $data): void;
}
