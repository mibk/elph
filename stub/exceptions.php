<?php

interface Throwable
{
	function getMessage(): string;

	function getCode(): int;
}

class Exception
{
	function getMessage(): string;

	function getCode(): int;
}

class RuntimeException extends Exception
{
}

class UnexpectedValueException extends Exception
{
}

class LogicException extends Exception
{
}

class InvalidArgumentException extends LogicException
{
}
