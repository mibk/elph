<?php

class DateTimeImmutable
{
	static function createFromFormat(string $format, string $datetime): DateTimeImmutable|false;

	function modify(string $modifier): static;

	function diff(DateTimeInterface $targetObject, bool $absolute = false): DateInterval;

	function format(string $format): string;

	function createFromInterface(DateTimeInterface $object): DateTimeImmutable;
}
