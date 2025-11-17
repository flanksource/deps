#!/usr/bin/env node
/**
 * Simple JavaScript script example for deps run
 */

const os = require('os');

function main() {
    console.log(`Hello from Node.js ${process.version}!`);
    console.log(`Platform: ${os.type()} ${os.arch()}`);

    const args = process.argv.slice(2);
    if (args.length > 0) {
        console.log(`Arguments: ${args.join(' ')}`);
    }

    return 0;
}

process.exit(main());
