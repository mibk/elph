<?php

class ZipArchive
{
	function open(string $filename, int $flags = 0): bool|int;

	function close(): bool;

	function addFromString(string $name, string $content, int $flags): bool;

	function addFile(string $filepath, string $entryname, int $start, int $length, int $flags);

	function getFromName(string $name, int $len, int $flags): string|false;

	function deleteName(string $name): bool;

	function setCompressionName(string $name, int $method, int $compflags): bool;
}
