<?php

class DateTimeImmutable
{
	function modify(string $modifier): static;

	function format(string $format): string;
}
