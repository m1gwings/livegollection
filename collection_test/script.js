let i = 0;

window.onload = function() {
    let inbox = document.getElementById('inbox');
    let sendButton = document.getElementById('send');
    let stringInput = document.getElementById('string');
    let numInput = document.getElementById('num');

    function addMessage(mess) {
        let messTextNode = document.createTextNode(`#${i} ${mess}`);
        i++;
        let messP = document.createElement('p');
        messP.appendChild(messTextNode);
        inbox.appendChild(messP);
    }

    let ws = new WebSocket('ws://localhost:8080/ws');

    ws.onmessage = function(event) {
        addMessage(event.data);
    }

    ws.onopen = function() {
        sendButton.onclick = function() {
            ws.send(JSON.stringify({"method":"CREATE","item":{"string":stringInput.value,"num":Number(numInput.value)}}))
        }
    }
}