#!/usr/bin/env node
/**
 * Simple test script to verify Node.js runtime execution.
 */

console.log("Node.js Runtime Test");
console.log(`Node version: ${process.version}`);
console.log(`Working directory: ${process.cwd()}`);

// Check for environment variables
const apiKey = process.env.TEST_API_KEY || 'not_set';
console.log(`TEST_API_KEY: ${apiKey}`);

console.log("Test completed successfully!");
