<?php

class DateInterval
{
	public int $y;
	public int $m;
	public int $d;
	public int $h;
	public int $i;
	public int $s;
	public float $f;
	public int $invert;
	public mixed $days;
	public bool $from_string;
	public string $date_string;

	function format(string $format): string;
}
