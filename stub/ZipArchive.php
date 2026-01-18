<?php

class ZipArchive
{
	function open(string $filename, int $flags = 0): bool|int;

	function close(): bool;

	function addFromString(string $name, string $content, int $flags): bool;
}
