<?php

class DateTime
{
	static function createFromFormat(string $format, string $datetime): DateTime|false;

	function modify(string $modifier): static;

	function diff(DateTimeInterface $targetObject, bool $absolute = false): DateInterval;

	function format(string $format): string;

	function getTimestamp(): int;

	function setTimestamp(int $timestamp);

	function setTime(int $hour, int $minute, int $second = 0, int $microsecond = 0): DateTime;

	function getLastErrors(): array|false
}
