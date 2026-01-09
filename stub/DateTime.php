<?php

class DateTime
{
	function modify(string $modifier): static;

	function diff(DateTimeInterface $targetObject, bool $absolute = false): DateInterval;

	function format(string $format): string;
}
