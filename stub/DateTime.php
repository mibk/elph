<?php

class DateTime
{
	static function createFromFormat(string $format, string $datetime): DateTime|false;

	static function createFromImmutable(DateTimeImmutable $object): DateTime;

	function modify(string $modifier): static;

	function diff(DateTimeInterface $targetObject, bool $absolute = false): DateInterval;

	function format(string $format): string;

	function getTimestamp(): int;

	function setTimestamp(int $timestamp);

	function setDate(int $year, int $month, int $day): DateTime;

	function setTime(int $hour, int $minute, int $second = 0, int $microsecond = 0): DateTime;

	function getLastErrors(): array|false
}
